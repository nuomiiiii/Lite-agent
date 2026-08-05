package cmd

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"reflect"
	"strconv"
	"strings"
	"syscall"

	"github.com/komari-monitor/komari-agent/dnsresolver"
	"github.com/komari-monitor/komari-agent/monitoring/netstatic"
	monitoring "github.com/komari-monitor/komari-agent/monitoring/unit"
	"github.com/komari-monitor/komari-agent/runtimeconfig"
	"github.com/komari-monitor/komari-agent/server"
	"github.com/komari-monitor/komari-agent/update"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	pkg_flags "github.com/komari-monitor/komari-agent/cmd/flags"
)

var flags = pkg_flags.GlobalConfig

var RootCmd = &cobra.Command{
	Use:   "komari-agent",
	Short: "komari agent",
	Long:  `komari agent`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := loadEffectiveConfig(cmd, flags); err != nil {
			return err
		}
		if flags.ProtocolVersion == 0 {
			flags.ProtocolVersion = 2
		}
		if err := validateRuntimeConfig(flags); err != nil {
			return err
		}
		runtimeconfig.Initialize(runtimeconfig.State{
			MonthRotate:        flags.MonthRotate,
			Interval:           flags.Interval,
			IncludeNics:        flags.IncludeNics,
			ExcludeNics:        flags.ExcludeNics,
			IncludeMountpoints: flags.IncludeMountpoints,
			MemoryIncludeCache: flags.MemoryIncludeCache,
			EnableGPU:          flags.EnableGPU,
		})
		// 捕获中止信号，优雅退出
		stopCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		go func() {
			<-stopCtx.Done()
			log.Printf("shutting down gracefully...")
			netstatic.Stop()
			os.Exit(0)
		}()

		if flags.ShowWarning {
			ShowToast()
			os.Exit(0)
		}

		if !flags.DisableWebSsh {
			go WarnKomariRunning()
		}

		if flags.MonthRotate != 0 {
			err := netstatic.StartOrContinue()
			if err != nil {
				log.Println("Failed to start netstatic monitoring:", err)
			}
			nics, err := monitoring.InterfaceList()
			if err != nil {
				log.Println("Failed to get interface list for netstatic:", err)
			}
			err = netstatic.SetNewConfig(netstatic.NetStaticConfig{
				Nics: nics,
			})
			if err != nil {
				log.Println("Failed to set netstatic config:", err)
			}
		}

		log.Println("Komari Agent", update.CurrentVersion)
		log.Println("Github Repo:", update.Repo)

		// 设置 DNS 解析行为
		if flags.CustomDNS != "" {
			dnsresolver.SetCustomDNSServer(flags.CustomDNS)
			log.Printf("Using custom DNS server: %s", flags.CustomDNS)
		} else {
			// 未设置则使用系统默认 DNS（不使用内置列表）
			log.Printf("Using system default DNS resolver")
		}

		// Auto discovery
		if flags.AutoDiscoveryKey != "" {
			err := handleAutoDiscovery()
			if err != nil {
				return fmt.Errorf("auto-discovery failed: %w", err)
			}
		}
		diskList, err := monitoring.DiskList()
		if err != nil {
			log.Println("Failed to get disk list:", err)
		}
		log.Println("Monitoring Mountpoints:", diskList)
		interfaceList, err := monitoring.InterfaceList()
		if err != nil {
			log.Println("Failed to get interface list:", err)
		}
		log.Println("Monitoring Interfaces:", interfaceList)

		// 忽略不安全的证书
		if flags.IgnoreUnsafeCert {
			http.DefaultTransport.(*http.Transport).TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
		}
		// 自动更新
		if flags.DisableAutoUpdate {
			log.Println("Automatic updates are disabled by configuration.")
		} else {
			initialUpdateFailed := false
			if err := update.CheckAndUpdate(); err != nil {
				log.Println("[ERROR]", err)
				initialUpdateFailed = true
			}
			go update.DoUpdateWorks(initialUpdateFailed)
		}
		go server.DoUploadBasicInfoWorks()
		go server.DoRuntimeConfigStateUploadWorks()
		for {
			server.UpdateBasicInfo()
			server.EstablishWebSocketConnection()
		}
	},
}

type explicitFlagValue struct {
	name  string
	value string
}

func loadEffectiveConfig(cmd *cobra.Command, config *pkg_flags.Config) error {
	explicitFlags := make([]explicitFlagValue, 0)
	cmd.Flags().Visit(func(flag *pflag.Flag) {
		explicitFlags = append(explicitFlags, explicitFlagValue{name: flag.Name, value: flag.Value.String()})
	})

	configPath := config.ConfigFile
	if envPath := strings.TrimSpace(os.Getenv("AGENT_CONFIG_FILE")); envPath != "" {
		configPath = envPath
	}
	for _, flag := range explicitFlags {
		if flag.name == "config" {
			configPath = flag.value
			break
		}
	}
	if configPath != "" {
		bytes, err := os.ReadFile(configPath)
		if err != nil {
			return fmt.Errorf("failed to read config file: %w", err)
		}
		if err := json.Unmarshal(bytes, config); err != nil {
			return fmt.Errorf("failed to parse config file: %w", err)
		}
	}

	loadFromEnv(config)
	for _, flag := range explicitFlags {
		if err := cmd.Flags().Set(flag.name, flag.value); err != nil {
			return fmt.Errorf("failed to restore command line flag --%s: %w", flag.name, err)
		}
	}
	return nil
}

func validateRuntimeConfig(config *pkg_flags.Config) error {
	if config.Interval <= 0 {
		return fmt.Errorf("invalid reporting interval %v: expected a value greater than 0", config.Interval)
	}
	if config.ReconnectInterval <= 0 {
		return fmt.Errorf("invalid reconnect interval %d: expected a value greater than 0", config.ReconnectInterval)
	}
	if config.InfoReportInterval <= 0 {
		return fmt.Errorf("invalid info report interval %d: expected a value greater than 0", config.InfoReportInterval)
	}
	if config.MaxRetries < 0 {
		return fmt.Errorf("invalid max retries %d: expected a non-negative value", config.MaxRetries)
	}
	if config.MonthRotate < 0 || config.MonthRotate > 31 {
		return fmt.Errorf("invalid month rotate day %d: expected 0 or a day from 1 to 31", config.MonthRotate)
	}
	if config.ProtocolVersion != 1 && config.ProtocolVersion != 2 {
		return fmt.Errorf("invalid protocol version %d: expected 1 or 2", config.ProtocolVersion)
	}
	if config.PreferIPVersion != "" && config.PreferIPVersion != "4" && config.PreferIPVersion != "6" {
		return fmt.Errorf("invalid preferred IP version %q: expected 4 or 6", config.PreferIPVersion)
	}
	if (config.CFAccessClientID == "") != (config.CFAccessClientSecret == "") {
		return fmt.Errorf("Cloudflare Access client ID and client secret must be configured together")
	}
	return nil
}

func Execute() {
	for i, arg := range os.Args {
		if arg == "-autoUpdate" || arg == "--autoUpdate" {
			log.Println("WARNING: The -autoUpdate flag is deprecated in version 0.0.9 and later. Use --disable-auto-update to configure auto-update behavior.")
			// 从参数列表中移除该参数，防止cobra解析错误
			os.Args = append(os.Args[:i], os.Args[i+1:]...)
			break
		}
		if arg == "-memory-mode-available" || arg == "--memory-mode-available" {
			//flags.MemoryIncludeCache = true
			log.Println("WARNING: The --memory-mode-available flag is deprecated in version 1.0.70 and later. Use --memory-include-cache to report memory usage including cache/buffer.")
			os.Args = append(os.Args[:i], os.Args[i+1:]...)
		}
	}

	if err := RootCmd.Execute(); err != nil {
		log.Println(err)
		os.Exit(1)
	}
}

func init() {
	RootCmd.PersistentFlags().StringVarP(&flags.Token, "token", "t", "", "API token")
	//RootCmd.MarkPersistentFlagRequired("token")
	RootCmd.PersistentFlags().StringVarP(&flags.Endpoint, "endpoint", "e", "", "API endpoint")
	//RootCmd.MarkPersistentFlagRequired("endpoint")
	RootCmd.PersistentFlags().StringVar(&flags.AutoDiscoveryKey, "auto-discovery", "", "Auto discovery key for the agent")
	RootCmd.PersistentFlags().BoolVar(&flags.DisableAutoUpdate, "disable-auto-update", false, "Disable automatic updates")
	RootCmd.PersistentFlags().BoolVar(&flags.DisableWebSsh, "disable-web-ssh", false, "Disable remote control(web ssh and rce)")
	//RootCmd.PersistentFlags().BoolVar(&flags.MemoryModeAvailable, "memory-mode-available", false, "[deprecated]Report memory as available instead of used.")
	RootCmd.PersistentFlags().Float64VarP(&flags.Interval, "interval", "i", 3.0, "Interval in seconds")
	RootCmd.PersistentFlags().BoolVarP(&flags.IgnoreUnsafeCert, "ignore-unsafe-cert", "u", false, "Ignore unsafe certificate errors")
	RootCmd.PersistentFlags().IntVarP(&flags.MaxRetries, "max-retries", "r", 3, "Maximum number of retries")
	RootCmd.PersistentFlags().IntVarP(&flags.ReconnectInterval, "reconnect-interval", "c", 5, "Reconnect interval in seconds")
	RootCmd.PersistentFlags().IntVar(&flags.InfoReportInterval, "info-report-interval", 5, "Interval in minutes for reporting basic info")
	RootCmd.PersistentFlags().StringVar(&flags.IncludeNics, "include-nics", "", "Comma-separated list of network interfaces to include")
	RootCmd.PersistentFlags().StringVar(&flags.ExcludeNics, "exclude-nics", "", "Comma-separated list of network interfaces to exclude")
	RootCmd.PersistentFlags().StringVar(&flags.IncludeMountpoints, "include-mountpoint", "", "Semicolon-separated list of mount points to include for disk statistics")
	RootCmd.PersistentFlags().IntVar(&flags.MonthRotate, "month-rotate", 0, "Month reset for network statistics (0 to disable)")
	RootCmd.PersistentFlags().StringVar(&flags.CFAccessClientID, "cf-access-client-id", "", "Cloudflare Access service-token Client ID")
	RootCmd.PersistentFlags().StringVar(&flags.CFAccessClientSecret, "cf-access-client-secret", "", "Cloudflare Access service-token Client Secret")
	RootCmd.PersistentFlags().BoolVar(&flags.MemoryIncludeCache, "memory-include-cache", false, "Include cache/buffer in memory usage")
	RootCmd.PersistentFlags().BoolVar(&flags.MemoryReportRawUsed, "memory-exclude-bcf", false, "Use \"raminfo.Used = v.Total - v.Free - v.Buffers - v.Cached\" calculation for memory usage")
	RootCmd.PersistentFlags().StringVar(&flags.CustomDNS, "custom-dns", "", "Custom DNS server to use (e.g. 8.8.8.8, 114.114.114.114). By default, the program uses the system DNS resolver.")
	RootCmd.PersistentFlags().BoolVar(&flags.EnableGPU, "gpu", false, "Enable detailed GPU monitoring (usage, memory, multi-GPU support)")
	RootCmd.PersistentFlags().BoolVar(&flags.ShowWarning, "show-warning", false, "Show security warning on Windows, run once as a subprocess")
	RootCmd.PersistentFlags().StringVar(&flags.CustomIpv4, "custom-ipv4", "", "Custom IPv4 address to use")
	RootCmd.PersistentFlags().StringVar(&flags.CustomIpv6, "custom-ipv6", "", "Custom IPv6 address to use")
	RootCmd.PersistentFlags().BoolVar(&flags.GetIpAddrFromNic, "get-ip-addr-from-nic", false, "Get IP address from network interface")
	RootCmd.PersistentFlags().StringVar(&flags.ConfigFile, "config", "", "Path to the configuration file")
	RootCmd.PersistentFlags().IntVar(&flags.ProtocolVersion, "protocol-version", 2, "Report protocol version (1 or 2)")
	RootCmd.PersistentFlags().BoolVar(&flags.DisableCompression, "disable-compression", false, "Disable v2 gzip/permessage-deflate compression")
	RootCmd.PersistentFlags().StringVar(&flags.PreferIPVersion, "prefer-ip-version", "", "Prefer IP version for dashboard connections: 4 or 6")
	RootCmd.PersistentFlags().ParseErrorsWhitelist.UnknownFlags = true
}

func loadFromEnv(config *pkg_flags.Config) {
	val := reflect.ValueOf(config).Elem()
	typ := val.Type()

	for i := 0; i < val.NumField(); i++ {
		field := val.Field(i)
		fieldType := typ.Field(i)

		// Get the env tag
		envTag := fieldType.Tag.Get("env")
		if envTag == "" {
			continue
		}

		// Get the environment variable value
		envValue := os.Getenv(envTag)
		if envValue == "" {
			continue
		}

		// Set the field based on its type
		switch field.Kind() {
		case reflect.String:
			field.SetString(envValue)
		case reflect.Bool:
			if boolVal, err := strconv.ParseBool(strings.TrimSpace(envValue)); err == nil {
				field.SetBool(boolVal)
			}
		case reflect.Int:
			if intVal, err := strconv.Atoi(envValue); err == nil {
				field.SetInt(int64(intVal))
			}
		case reflect.Float64:
			if floatVal, err := strconv.ParseFloat(envValue, 64); err == nil {
				field.SetFloat(floatVal)
			}
		}
	}
}
