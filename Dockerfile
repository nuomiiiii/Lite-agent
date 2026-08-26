FROM alpine:3.21

WORKDIR /app

# Docker buildx 会在构建时自动填充这些变量
ARG TARGETOS
ARG TARGETARCH

COPY Lite-agent-${TARGETOS}-${TARGETARCH} /app/Lite-agent

RUN chmod +x /app/Lite-agent

# New marker is Lite-agent. Keep the komari-agent marker so upgraded
# containers still skip binary self-update.
RUN touch /.lite-agent-container /.komari-agent-container

ENTRYPOINT ["/app/Lite-agent"]
# 运行时请指定参数
# Please specify parameters at runtime.
# eg: docker run lite-agent -e example.com -t token
CMD ["--help"]
