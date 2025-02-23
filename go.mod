module github.com/jetkvm/kvm

go 1.21.0

toolchain go1.21.1

require (
	github.com/Masterminds/semver/v3 v3.3.0
	github.com/beevik/ntp v1.3.1
	github.com/coder/websocket v1.8.12
	github.com/coreos/go-oidc/v3 v3.11.0
	github.com/creack/pty v1.1.23
	github.com/gin-gonic/gin v1.9.1
	github.com/google/uuid v1.6.0
	github.com/gwatts/rootcerts v0.0.0-20240401182218-3ab9db955caf
	github.com/hanwen/go-fuse/v2 v2.5.1
	github.com/hashicorp/go-envparse v0.1.0
	github.com/openstadia/go-usb-gadget v0.0.0-20231115171102-aebd56bbb965
	github.com/pion/logging v0.2.2
	github.com/pion/mdns/v2 v2.0.7
	github.com/pion/webrtc/v4 v4.0.0
	github.com/pojntfx/go-nbd v0.3.2
	github.com/psanford/httpreadat v0.1.0
	github.com/vishvananda/netlink v1.3.0
	go.bug.st/serial v1.6.2
	golang.org/x/crypto v0.28.0
	golang.org/x/net v0.30.0
)

replace github.com/pojntfx/go-nbd v0.3.2 => github.com/chemhack/go-nbd v0.0.0-20241006125820-59e45f5b1e7b

require (
	github.com/bytedance/sonic v1.11.6 // indirect
	github.com/bytedance/sonic/loader v0.1.1 // indirect
	github.com/cloudwego/base64x v0.1.4 // indirect
	github.com/cloudwego/iasm v0.2.0 // indirect
	github.com/creack/goselect v0.1.2 // indirect
	github.com/gabriel-vasile/mimetype v1.4.3 // indirect
	github.com/gin-contrib/sse v0.1.0 // indirect
	github.com/go-jose/go-jose/v4 v4.0.2 // indirect
	github.com/go-playground/locales v0.14.1 // indirect
	github.com/go-playground/universal-translator v0.18.1 // indirect
	github.com/go-playground/validator/v10 v10.20.0 // indirect
	github.com/goccy/go-json v0.10.2 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/klauspost/cpuid/v2 v2.2.7 // indirect
	github.com/kr/pretty v0.3.0 // indirect
	github.com/leodido/go-urn v1.4.0 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	github.com/pelletier/go-toml/v2 v2.2.2 // indirect
	github.com/pilebones/go-udev v0.9.0 // indirect
	github.com/pion/datachannel v1.5.9 // indirect
	github.com/pion/dtls/v3 v3.0.3 // indirect
	github.com/pion/ice/v4 v4.0.2 // indirect
	github.com/pion/interceptor v0.1.37 // indirect
	github.com/pion/randutil v0.1.0 // indirect
	github.com/pion/rtcp v1.2.14 // indirect
	github.com/pion/rtp v1.8.9 // indirect
	github.com/pion/sctp v1.8.33 // indirect
	github.com/pion/sdp/v3 v3.0.9 // indirect
	github.com/pion/srtp/v3 v3.0.4 // indirect
	github.com/pion/stun/v3 v3.0.0 // indirect
	github.com/pion/transport/v3 v3.0.7 // indirect
	github.com/pion/turn/v4 v4.0.0 // indirect
	github.com/rogpeppe/go-internal v1.8.0 // indirect
	github.com/twitchyliquid64/golang-asm v0.15.1 // indirect
	github.com/ugorji/go/codec v1.2.12 // indirect
	github.com/vishvananda/netns v0.0.4 // indirect
	github.com/wlynxg/anet v0.0.5 // indirect
	golang.org/x/arch v0.8.0 // indirect
	golang.org/x/oauth2 v0.21.0 // indirect
	golang.org/x/sys v0.29.0 // indirect
	golang.org/x/text v0.19.0 // indirect
	google.golang.org/protobuf v1.34.0 // indirect
	gopkg.in/check.v1 v1.0.0-20201130134442-10cb98267c6c // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)
