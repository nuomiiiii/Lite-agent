#!/bin/bash

# Migrate an existing komari-agent install to Lite-agent.
# Keeps the original endpoint, node token, and launch flags, then
# uninstalls komari-agent after Lite-agent is running.

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
CYAN='\033[0;36m'
WHITE='\033[1;37m'
NC='\033[0m'

log_info() { echo -e "${NC} $1"; }
log_success() { echo -e "${GREEN}${NC} $1"; }
log_warning() { echo -e "${YELLOW}[WARNING]${NC} $1"; }
log_error() { echo -e "${RED}[ERROR]${NC} $1"; }
log_step() { echo -e "${NC} $1"; }
log_config() { echo -e "${CYAN}[CONFIG]${NC} $1"; }

legacy_service_name="komari-agent"
github_repo="nuomiiiii/Lite-agent"
github_proxy=""
install_version=""
endpoint_override=""
collected=()
source_dirs=()

os_type=$(uname -s)
case $os_type in
    Darwin)
        os_name="darwin"
        target_dir="/usr/local/lite-agent"
        if [ ! -w "/usr/local" ] && [ "$EUID" -ne 0 ]; then
            target_dir="$HOME/.lite-agent"
        fi
        ;;
    Linux)
        os_name="linux"
        target_dir="/opt/lite-agent"
        ;;
    FreeBSD)
        os_name="freebsd"
        target_dir="/opt/lite-agent"
        ;;
    MINGW*|MSYS*|CYGWIN*)
        os_name="windows"
        target_dir="/c/lite-agent"
        ;;
    *)
        log_error "Unsupported operating system: $os_type"
        exit 1
        ;;
esac

while [ $# -gt 0 ]; do
    case $1 in
        --install-ghproxy)
            github_proxy="$2"
            shift 2
            ;;
        --install-version)
            install_version="$2"
            shift 2
            ;;
        --install-dir|--install-service-name)
            log_error "Migration always uses the default Lite-agent directory and service name."
            log_error "Run this script without $1 so komari-agent can be uninstalled afterward."
            exit 1
            ;;
        --install*)
            log_warning "Unknown install parameter: $1"
            shift
            ;;
        -e|--endpoint)
            endpoint_override="$2"
            shift 2
            ;;
        --endpoint=*)
            endpoint_override="${1#--endpoint=}"
            shift
            ;;
        -t|--token|--token=*)
            log_error "Do not pass a new token. This script keeps the existing komari-agent key."
            exit 1
            ;;
        *)
            log_error "Unknown argument: $1"
            log_error "Usage: migrate.sh [--endpoint URL] [--install-ghproxy URL] [--install-version VER]"
            exit 1
            ;;
    esac
done

require_root_for_deps=true
if [ "$os_name" = "darwin" ] && command -v brew >/dev/null 2>&1; then
    require_root_for_deps=false
fi
if [ "$EUID" -ne 0 ] && [ "$require_root_for_deps" = true ]; then
    log_error "Please run as root"
    exit 1
fi

if [ -f /.lite-agent-container ] || [ -f /.komari-agent-container ] || [ -f /.dockerenv ]; then
    log_error "This host looks like a container. Recreate the Lite-agent container with the original -e and -t."
    exit 1
fi

add_source_dir() {
    local dir="$1"
    [ -n "$dir" ] || return 0
    [ -d "$dir" ] || return 0
    case "$dir" in
        /|/usr|/usr/local|/opt|/bin|/sbin) return 0 ;;
    esac
    local existing
    for existing in "${source_dirs[@]+"${source_dirs[@]}"}"; do
        if [ "$existing" = "$dir" ]; then
            return 0
        fi
    done
    source_dirs+=("$dir")
    return 0
}

split_cmdline() {
    local line="$1"
    local out=""
    local py
    [ -n "$line" ] || return 0
    for py in python3 python; do
        if command -v "$py" >/dev/null 2>&1; then
            if out=$("$py" -c 'import shlex,sys
parts=shlex.split(sys.argv[1])
if not parts:
    raise SystemExit(1)
for part in parts:
    print(part)' "$line" 2>/dev/null) && [ -n "$out" ]; then
                printf '%s\n' "$out"
                return 0
            fi
        fi
    done
    # shellcheck disable=SC2086
    eval "set -- $line"
    printf '%s\n' "$@"
}

append_args_from_cmdline() {
    local line="$1"
    local skip_first="${2:-0}"
    local word
    local first=1
    while IFS= read -r word; do
        [ -n "$word" ] || continue
        if [ "$skip_first" = "1" ] && [ "$first" = "1" ]; then
            first=0
            add_source_dir "$(dirname "$word")"
            continue
        fi
        first=0
        collected+=("$word")
    done <<EOF
$(split_cmdline "$line")
EOF
}

json_field() {
    local file="$1"
    local key="$2"
    local py
    [ -f "$file" ] || return 0
    for py in python3 python; do
        if command -v "$py" >/dev/null 2>&1 && "$py" -c 'import json' >/dev/null 2>&1; then
            "$py" -c 'import json,sys
try:
    data=json.load(open(sys.argv[1]))
except Exception:
    raise SystemExit(0)
value=data.get(sys.argv[2], "")
if isinstance(value, str):
    sys.stdout.write(value)' "$file" "$key"
            return
        fi
    done
    sed -n "s/.*\"${key}\"[[:space:]]*:[[:space:]]*\"\\([^\"]*\\)\".*/\\1/p" "$file" | head -n1
}

flag_value_from_args() {
    local want="$1"
    local short="$2"
    local prev=""
    local token
    for token in "${collected[@]+"${collected[@]}"}"; do
        if [ -n "$short" ] && [ "$prev" = "-$short" ]; then
            printf '%s' "$token"
            return 0
        fi
        if [ "$prev" = "--$want" ]; then
            printf '%s' "$token"
            return 0
        fi
        case "$token" in
            --"$want"=*)
                printf '%s' "${token#--$want=}"
                return 0
                ;;
        esac
        prev="$token"
    done
    return 1
}

has_named_flag() {
    local want="$1"
    local short="$2"
    local token
    for token in "${collected[@]+"${collected[@]}"}"; do
        if [ -n "$short" ] && [ "$token" = "-$short" ]; then
            return 0
        fi
        case "$token" in
            --"$want"|--"$want"=*)
                return 0
                ;;
        esac
    done
    return 1
}

replace_or_add_flag() {
    local name="$1"
    local short="$2"
    local value="$3"
    local out=()
    local skip=0
    local token
    for token in "${collected[@]+"${collected[@]}"}"; do
        if [ "$skip" = "1" ]; then
            skip=0
            continue
        fi
        if [ -n "$short" ] && [ "$token" = "-$short" ]; then
            skip=1
            continue
        fi
        if [ "$token" = "--$name" ]; then
            skip=1
            continue
        fi
        case "$token" in
            --"$name"=*)
                continue
                ;;
        esac
        out+=("$token")
    done
    if [ -n "$short" ]; then
        out+=("-$short" "$value")
    else
        out+=("--$name" "$value")
    fi
    collected=("${out[@]}")
}

drop_named_flag() {
    local name="$1"
    local out=()
    local skip=0
    local token
    for token in "${collected[@]+"${collected[@]}"}"; do
        if [ "$skip" = "1" ]; then
            skip=0
            continue
        fi
        if [ "$token" = "--$name" ]; then
            skip=1
            continue
        fi
        case "$token" in
            --"$name"=*)
                continue
                ;;
        esac
        out+=("$token")
    done
    collected=("${out[@]+"${out[@]}"}")
}

redact_collected() {
    local out="" prev="" token
    for token in "${collected[@]+"${collected[@]}"}"; do
        if [ "$prev" = "-t" ] || [ "$prev" = "--token" ] || [ "$prev" = "--cf-access-client-secret" ]; then
            out="$out ***"
            prev="$token"
            continue
        fi
        case "$token" in
            --token=*)
                out="$out --token=***"
                prev=""
                continue
                ;;
            --cf-access-client-secret=*)
                out="$out --cf-access-client-secret=***"
                prev=""
                continue
                ;;
        esac
        out="$out $token"
        prev="$token"
    done
    echo "${out# }"
}

read_env_assignment() {
    local blob="$1"
    local key="$2"
    local token
    # shellcheck disable=SC2086
    for token in $blob; do
        case "$token" in
            "$key"=*)
                printf '%s' "${token#*=}"
                return 0
                ;;
        esac
    done
    return 1
}

read_systemd_legacy() {
    command -v systemctl >/dev/null 2>&1 || return 1
    systemctl cat "${legacy_service_name}.service" >/dev/null 2>&1 || return 1
    local raw argv wd envblob fragment bindir
    raw=$(systemctl show "${legacy_service_name}.service" -p ExecStart --value 2>/dev/null || true)
    case "$raw" in
        *argv[]=*)
            argv="${raw#*argv[]=}"
            argv="${argv%% ;*}"
            if [ -n "$argv" ]; then
                append_args_from_cmdline "$argv" 1
            fi
            ;;
    esac
    if [ ${#collected[@]} -eq 0 ]; then
        fragment=$(systemctl show "${legacy_service_name}.service" -p FragmentPath --value 2>/dev/null || true)
        if [ -n "$fragment" ] && [ -f "$fragment" ]; then
            local line
            line=$(grep -E '^ExecStart=' "$fragment" | head -n1)
            line="${line#ExecStart=}"
            append_args_from_cmdline "$line" 1
        fi
    fi
    wd=$(systemctl show "${legacy_service_name}.service" -p WorkingDirectory --value 2>/dev/null || true)
    add_source_dir "$wd"
    envblob=$(systemctl show "${legacy_service_name}.service" -p Environment --value 2>/dev/null || true)
    if [ -n "$envblob" ]; then
        local env_token env_endpoint
        if env_token=$(read_env_assignment "$envblob" "AGENT_TOKEN"); then
            if ! has_named_flag token t; then
                collected+=("-t" "$env_token")
            fi
        fi
        if env_endpoint=$(read_env_assignment "$envblob" "AGENT_ENDPOINT"); then
            if ! has_named_flag endpoint e; then
                collected+=("-e" "$env_endpoint")
            fi
        fi
    fi
    bindir=$(systemctl show "${legacy_service_name}.service" -p ExecStart --value 2>/dev/null | sed -n 's/.*path=\([^ ;]*\).*/\1/p' | head -n1)
    add_source_dir "$(dirname "$bindir")"
    return 0
}

read_unit_file_args() {
    local file="$1"
    local line
    [ -f "$file" ] || return 1
    if line=$(grep -E '^(ARGS|command_args)=' "$file" | head -n1); then
        line="${line#ARGS=}"
        line="${line#command_args=}"
        line="${line#\"}"
        line="${line%\"}"
        append_args_from_cmdline "$line" 0
        return 0
    fi
    if line=$(grep -E '^exec ' "$file" | head -n1); then
        line="${line#exec }"
        append_args_from_cmdline "$line" 1
        return 0
    fi
    if line=$(grep -E '^command=' "$file" | head -n1); then
        line="${line#command=}"
        line="${line#\"}"
        line="${line%\"}"
        add_source_dir "$(dirname "$line")"
    fi
    return 1
}

read_launchd_legacy() {
    [ "$os_name" = "darwin" ] || return 1
    local plist
    for plist in \
        "/Library/LaunchDaemons/com.komari.${legacy_service_name}.plist" \
        "$HOME/Library/LaunchAgents/com.komari.${legacy_service_name}.plist"
    do
        [ -f "$plist" ] || continue
        add_source_dir "$(dirname "$(/usr/libexec/PlistBuddy -c 'Print :Program' "$plist" 2>/dev/null || true)")"
        local wd
        wd=$(/usr/libexec/PlistBuddy -c 'Print :WorkingDirectory' "$plist" 2>/dev/null || true)
        add_source_dir "$wd"
        if command -v python3 >/dev/null 2>&1 && command -v plutil >/dev/null 2>&1; then
            local json
            json=$(plutil -convert json -o - "$plist" 2>/dev/null || true)
            if [ -n "$json" ]; then
                while IFS= read -r word; do
                    [ -n "$word" ] || continue
                    collected+=("$word")
                done <<EOF
$(python3 -c 'import json,sys
data=json.loads(sys.argv[1])
args=data.get("ProgramArguments") or []
for item in args[1:]:
    print(item)' "$json")
EOF
                return 0
            fi
        fi
        local raw
        raw=$(/usr/libexec/PlistBuddy -c 'Print :ProgramArguments' "$plist" 2>/dev/null || true)
        if [ -n "$raw" ]; then
            local skip=1
            while IFS= read -r word; do
                word=$(printf '%s' "$word" | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')
                case "$word" in
                    ""|"Array {"|"}") continue ;;
                esac
                if [ "$skip" = "1" ]; then
                    skip=0
                    add_source_dir "$(dirname "$word")"
                    continue
                fi
                collected+=("$word")
            done <<EOF
$raw
EOF
            return 0
        fi
    done
    return 1
}

read_proc_legacy() {
    local pid cmd exe
    for pid in /proc/[0-9]*; do
        [ -r "$pid/cmdline" ] || continue
        exe=$(readlink "$pid/exe" 2>/dev/null || true)
        case "$exe" in
            */komari-agent|*/komari/agent|/opt/komari/*|/usr/local/komari/*)
                cmd=$(tr '\0' ' ' < "$pid/cmdline")
                append_args_from_cmdline "$cmd" 1
                add_source_dir "$(dirname "$exe")"
                return 0
                ;;
        esac
    done
    return 1
}

legacy_found=false
if read_systemd_legacy; then
    legacy_found=true
elif read_unit_file_args "/etc/init.d/${legacy_service_name}"; then
    legacy_found=true
elif read_unit_file_args "/etc/init/${legacy_service_name}.conf"; then
    legacy_found=true
elif read_launchd_legacy; then
    legacy_found=true
elif read_proc_legacy; then
    legacy_found=true
fi

add_source_dir "/opt/komari"
add_source_dir "/usr/local/komari"
add_source_dir "$HOME/.komari"
add_source_dir "/c/komari"

if [ "$legacy_found" != true ] && [ ${#collected[@]} -eq 0 ]; then
    log_error "No running komari-agent install was found."
    log_error "This script reads the existing komari-agent service and keeps its node token."
    exit 1
fi

rewrite_config_path() {
    local cfg dest
    if ! has_named_flag config ""; then
        return
    fi
    cfg=$(flag_value_from_args config "") || return 0
    [ -n "$cfg" ] || return 0
    [ -f "$cfg" ] || return 0
    mkdir -p "$target_dir"
    dest="$target_dir/$(basename "$cfg")"
    if [ "$cfg" != "$dest" ] && [ ! -f "$dest" ]; then
        log_info "Copying config $(basename "$cfg") to $target_dir"
        cp -a "$cfg" "$dest"
    fi
    if [ -f "$dest" ]; then
        replace_or_add_flag config "" "$dest"
    fi
}

sidecar_token=""
sidecar_endpoint=""
sidecar_uuid=""
for dir in "${source_dirs[@]+"${source_dirs[@]}"}"; do
    if [ -z "$sidecar_token" ]; then
        sidecar_token=$(json_field "$dir/auto-discovery.json" token)
    fi
    if [ -z "$sidecar_uuid" ]; then
        sidecar_uuid=$(json_field "$dir/auto-discovery.json" uuid)
    fi
    if [ -z "$sidecar_token" ]; then
        sidecar_token=$(json_field "$dir/node.json" token)
    fi
    if [ -z "$sidecar_endpoint" ]; then
        sidecar_endpoint=$(json_field "$dir/auto-discovery.json" endpoint)
    fi
    if [ -z "$sidecar_endpoint" ]; then
        sidecar_endpoint=$(json_field "$dir/node.json" endpoint)
    fi
done

if ! has_named_flag token t; then
    if [ -n "$sidecar_token" ]; then
        collected+=("-t" "$sidecar_token")
    else
        log_error "Could not find the existing node token in komari-agent arguments or identity files."
        log_error "Migration will not create a new Lite node. Keep the original key in place and try again."
        exit 1
    fi
fi

if [ -n "$endpoint_override" ]; then
    replace_or_add_flag endpoint e "$endpoint_override"
elif ! has_named_flag endpoint e; then
    if [ -n "$sidecar_endpoint" ]; then
        collected+=("-e" "$sidecar_endpoint")
    else
        log_error "Could not find the existing panel address. Pass --endpoint with your Lite URL."
        exit 1
    fi
fi

drop_named_flag auto-discovery
rewrite_config_path

copy_sidecars_from() {
    local src="$1"
    if [ ! -d "$src" ] || [ "$src" = "$target_dir" ]; then
        return
    fi
    mkdir -p "$target_dir"
    local name
    for name in auto-discovery.json net_static.json net_static.json.bak node.json remote-control.state; do
        if [ -f "$src/$name" ] && [ ! -f "$target_dir/$name" ]; then
            log_info "Copying $name from $src to $target_dir"
            cp -a "$src/$name" "$target_dir/$name"
        fi
    done
}

echo -e "${WHITE}===========================================${NC}"
echo -e "${WHITE}    Lite Agent Migration Script        ${NC}"
echo -e "${WHITE}===========================================${NC}"
echo ""
log_config "Keeping the original node token and installing Lite-agent"
log_config "  Endpoint: ${GREEN}$(flag_value_from_args endpoint e)${NC}"
if [ -n "$sidecar_uuid" ]; then
    log_config "  Saved UUID: ${GREEN}$sidecar_uuid${NC}"
fi
log_config "  Arguments: ${GREEN}$(redact_collected)${NC}"
echo ""

log_step "Copying sidecar files from the detected komari-agent directories..."
for dir in "${source_dirs[@]+"${source_dirs[@]}"}"; do
    copy_sidecars_from "$dir"
done

script_dir=""
if [ -n "${BASH_SOURCE[0]:-}" ] && [ -f "${BASH_SOURCE[0]}" ]; then
    script_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
fi

installer=""
if [ -n "$script_dir" ] && [ -f "$script_dir/install.sh" ]; then
    installer="$script_dir/install.sh"
else
    raw_url="https://raw.githubusercontent.com/${github_repo}/main/install.sh"
    if [ -n "$github_proxy" ]; then
        raw_url="${github_proxy%/}/https://raw.githubusercontent.com/${github_repo}/main/install.sh"
    fi
    installer=$(mktemp)
    trap 'rm -f "$installer"' EXIT
    log_step "Downloading Lite-agent install.sh..."
    if command -v curl >/dev/null 2>&1; then
        if ! curl -fsSL "$raw_url" -o "$installer"; then
            log_error "Failed to download install.sh"
            exit 1
        fi
    elif command -v wget >/dev/null 2>&1; then
        if ! wget -qO "$installer" "$raw_url"; then
            log_error "Failed to download install.sh"
            exit 1
        fi
    else
        log_error "Need curl or wget to download install.sh"
        exit 1
    fi
    if [ ! -s "$installer" ]; then
        log_error "Downloaded install.sh is empty"
        exit 1
    fi
fi

install_flags=()
if [ -n "$github_proxy" ]; then
    install_flags+=(--install-ghproxy "$github_proxy")
fi
if [ -n "$install_version" ]; then
    install_flags+=(--install-version "$install_version")
fi

log_step "Installing Lite-agent with the original node key..."
bash "$installer" "${install_flags[@]+"${install_flags[@]}"}" "${collected[@]}"
install_status=$?
if [ "$install_status" -ne 0 ]; then
    exit "$install_status"
fi
log_success "Migration finished. Confirm the node is online in Lite."
