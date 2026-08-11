# Telegram Popup — cloud image.
# Build:  docker build -t tgpopup .
# Run:    docker run -p 10000:10000 -e PORT=10000 -e TGPOPUP_KEY=secret tgpopup

FROM golang:1.24-alpine AS build
WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /out/tgpopup .

# Runtime: empty image + one static binary. No OS to patch, ~7MB total.
# Certificates come from the build stage; the timezone db is embedded in the
# binary (time/tzdata), so TZ works even here.
FROM scratch
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /out/tgpopup /app/tgpopup
COPY --from=build /src/dist/config.json /app/config.json
WORKDIR /app
ENV TZ=Asia/Jerusalem
# PORT is provided by the platform (Render sets it automatically).
# TGPOPUP_KEY protects the page; TGPOPUP_CHANNELS overrides the channel list.
CMD ["/app/tgpopup"]
