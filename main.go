package main

import (
	"flag"
	"log"
	"os"
	"strings"

	"github.com/ge9/ts-proxy/tsproxy"
	"tailscale.com/tsnet"
)

// 転送ルールを保持する構造体
type forwardRule struct {
	Bind    string
	Connect string
}

type socksServeRule struct {
	Bind string
	Out4 string
	Out6 string
}

// flagパッケージ用のカスタム型定義
type stringList []string

func (s *stringList) String() string {
	return strings.Join(*s, ", ")
}
func (s *stringList) Set(value string) error {
	*s = append(*s, value)
	return nil
}

func main() {
	var (
		debug       bool
		tcpTimeout  = 1100
		udpTimeout  = 330
		tsServer    *tsnet.Server
		hostname    string
		tsdir       string
		tcpRulesRaw stringList
		udpRulesRaw stringList
		fSocksRaw   stringList // forwardSOCKS
		sSocksRaw   stringList // serveSOCKS
	)

	flag.StringVar(&hostname, "hostname", "ts-proxy", "Tailscale device hostname")
	flag.StringVar(&tsdir, "tsnet-dir", "", "Directory for Tailscale credentials")
	flag.IntVar(&tcpTimeout, "tcp-timeout", tcpTimeout, "TCP timeout in seconds")
	flag.IntVar(&udpTimeout, "udp-timeout", udpTimeout, "UDP timeout in seconds")
	flag.BoolVar(&debug, "debug", debug, "enable debug mode")

	flag.Var(&tcpRulesRaw, "tcp", "TCP forward rule: 'bind_addr=connect_addr'")
	flag.Var(&udpRulesRaw, "udp", "UDP forward rule: 'bind_addr=connect_addr'")
	flag.Var(&fSocksRaw, "fwd-socks", "Forward SOCKS: 'bind_addr=tailscale_addr'")
	flag.Var(&sSocksRaw, "serve-socks", "Serve SOCKS: 'tailscale_addr[,outaddr_config...]'")
	flag.Parse()
	if flag.NFlag() == 0 {
		flag.Usage()
		os.Exit(0)
	}

	tsServer = &tsnet.Server{
		Hostname: hostname,
		Dir:      tsdir,
		// Ephemeral: true,
		Logf: func(format string, args ...any) {
			if debug {
				log.Printf(format, args...)
			}
		},
	}
	defer tsServer.Close()

	tsproxy.Setup(tsServer, tcpTimeout, udpTimeout, debug)

	// --- TCP Forwarding ---
	for _, raw := range tcpRulesRaw {
		var useTLS bool
		parts := strings.Split(raw, "=TLS=")
		if len(parts) != 2 {
			parts = strings.Split(raw, "=")
			if len(parts) != 2 {
				log.Printf("Invalid TCP rule format: %s", raw)
				continue
			}
		} else {
			useTLS = true
		}
		go tsproxy.ForwardTCP(strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]), useTLS)
	}

	for _, raw := range udpRulesRaw {
		parts := strings.Split(raw, "=")
		if len(parts) != 2 {
			log.Printf("Invalid UDP rule format: %s", raw)
			continue
		}
		go tsproxy.ForwardUDP(strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]))
	}

	for _, raw := range fSocksRaw {
		parts := strings.Split(raw, "=")
		if len(parts) != 2 {
			log.Printf("Invalid Forward SOCKS format: %s", raw)
			continue
		}
		go tsproxy.ForwardSOCKS(strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]))
	}

	for _, raw := range sSocksRaw {
		parts := strings.Split(raw, ",")
		if len(parts) == 0 {
			continue
		}
		parts0 := parts[0]
		if parts0[:1] == ":" { //support shorthand notation
			parts0 = hostname + ".tshost" + parts0
		}
		var tcp4, tcp6, udp4, udp6 string // empty string as default

		for _, opt := range parts[1:] {
			kv := strings.SplitN(opt, "=", 2)
			if len(kv) != 2 {
				log.Printf("Warning: Invalid option format in serve-socks: %s", opt)
				continue
			}
			key := strings.TrimSpace(kv[0])
			val := strings.TrimSpace(kv[1])
			switch key {
			case "tcp4":
				tcp4 = val
			case "tcp6":
				tcp6 = val
			case "udp4":
				udp4 = val
			case "udp6":
				udp6 = val
			case "ip4":
				tcp4 = val
				udp4 = val
			case "ip6":
				tcp6 = val
				udp6 = val
			default:
				log.Printf("Warning: Unknown key in serve-socks: %s", key)
			}
		}
		go tsproxy.ServeSOCKS(strings.TrimSpace(parts0), tcp4+":0", tcp6+":0", udp4+":0", udp6+":0")
	}

	select {}
}
