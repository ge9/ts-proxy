    //to forward http3 udp:
    TS_DEBUG_MTU=1400 ./ts-proxy  -udp <relay ts ip>:8443=<target ts ip>:8443


# ts-proxy
`ts-proxy` is a userspace Tailscale client that provides TCP/UDP port-forwarding and SOCKS5 proxy with UDP support. Full cone NAT and no UDP-over-TCP for SOCKS5 UDP (as long as Tailscale is not falling back on DERP).

# Usage
```
Usage of ts-proxy:
  -debug
        enable debug mode
  -dual-socks value
        Combination of "Tailnet SOCKS" and "Serve SOCKS"
  -ephemeral
        use ephemeral node
  -fwd-socks value
        Forward SOCKS: 'bind_addr=tailscale_addr'
  -hostname string
        Tailscale device hostname (default "ts-proxy")
  -serve-socks value
        Serve SOCKS: 'bind_addr[,outaddr_config...]'
  -tags string
        comma-separated tags
  -tailnet-socks value
        Serve Tailnet SOCKS: 'bind_addr'
  -tcp value
        TCP forward rule: 'bind_addr=connect_addr'
  -tcp-timeout int
        TCP timeout in seconds (default 1100)
  -tsnet-dir string
        Directory for Tailscale credentials
  -udp value
        UDP forward rule: 'bind_addr=connect_addr'
  -udp-timeout int
        UDP timeout in seconds (default 330)
```

## General
`bind_addr`, `connect_addr`, and `tailscale_addr` share basically the same syntax: `host:port`.
If a host string ends with `.tshost`, it will be replaced by the corresponding IP(v4) address using Tailscale DNS.
`.tshost` can be a shorthand for `[hostname].tshost`.
SOCKS and forward options can be specified multiple times.

`-tcp-timeout` sets the TCP timeout in SOCKS5. `-udp-timeout` sets the UDP timeout for both UDP port forwarding and SOCKS5 UDP Associate.
`-tsnet-dir` and `-hostname` are Tailscale-specific options. 

## Port Forwarding (`-tcp`, `-udp`)
If addresses (possibly resolved from `.tshost`) are within the Tailscale IP range (`100.64.0.0/10` or `fd7a:115c:a1e0::/64`), they will be bound or connected via Tailscale.
`bind_addr` accepts a port-only specification (e.g., `:8080`). `connect_addr` accepts a domain specification (e.g., `example.com:80`).
Port forwarding between two local addresses or two Tailscale addresses is also possible (though less useful).

For TCP forwarding, using `=TLS=` instead of `=` enables TLS termination. You must enable HTTPS in the Tailscale Admin Console for this to work.

## SOCKS5 serving (`-serve-socks`)
Serves a SOCKS5 proxy. `bind_addr` can be followed by a comma-separated list of `outaddr_config` entries, which specify outgoing addresses for the SOCKS5 proxy.

Each `outaddr_config` must follow the `scope=ip` syntax, where `scope` is one of: `tcp4`, `tcp6`, `udp4`, `udp6`, `ip4` (sets both `tcp4` and `udp4`), or `ip6` (sets both `tcp6` and `udp6`).
Either `udp4` or `udp6` can be set to `disabled` to avoid potential performance issues with `delayedUDPConn`.

## SOCKS5 forwarding (`-fwd-socks`)
Serves a SOCKS5 proxy on `bind_addr` that forwards traffic to an upstream SOCKS5 proxy specified by `tailscale_addr`.
`bind_addr` has the same syntax as in `-tcp` and `-udp`. `tailscale_addr` must be a Tailscale address.

## SOCKS5 via Tailnet (`-tailnet-socks`)
Serves a SOCKS5 proxy on `bind_addr` that forwards traffic to Tailnet. `bind_addr` has the same syntax as in `-tcp` and `-udp`. Traffic whose destination is not Tailscale address is routed via local network.

## "Dual" SOCKS5 (`-dual-socks`)
A combination of `-serve-socks` and `-tailnet-socks`. Similar to `-tailnet-socks`, but accepts outgoing address configuration.

In UDP, `delayedUDPConn` is always used and `disabled` is not allowed in `udp4` or `udp6`.

## Environment variables
tsnet (which is used in ts-proxy) accepts some environment variables relating auth key, oauth client, etc. See tsnet documentation for detail.

# Example

## SOCKS5 as Exit Node

- on one device
  ```
  ts-proxy -hostname pc1 -serve-socks .tshost:1080,tcp4=10.2.0.2,tcp6=[2400::1111]
  ```
  Outgoing addresses are optional.
- on another device
  If Tailscale is running on the device, 100.xxx.yyy.zzz:1080 (according to the Tailscale IP of `pc1`) is SOCKS5 proxy port.
  If not, ts-proxy can be used again to serve SOCKS5 proxy locally.
  ```
  ts-proxy -hostname pc2 -fwd-socks localhost:1080=pc1.tshost:1080
  ```

## Serve HTTP as HTTPS
```
ts-proxy -hostname pc1 -tcp .tshost:443=TLS=localhost:8080 -tailnet-socks localhost:1080
```
If Tailscale is running on the device, https://pc1.tailXXXXX.ts.net is directly accessible in the browser.
If not, `localhost:1080` should be set as SOCKS5 proxy for the browser (possibly with per-domain switching extensions/addons).
Another possible setup is serving on localhost and editing `/etc/hosts`.
```
ts-proxy -hostname pc1 -tcp localhost:8443=TLS=localhost:8080
```
The first access will take some time waiting for TLS certificate issued by Let's Encrypt.

# How it works
tsnet handles all Tailscale connectivity. https://github.com/txthinking/socks5 with minor customizations is used for the SOCKS5 server/client.

## Fix for Android (Termux)
Due to https://github.com/golang/go/issues/40569, `net.Interface()` and `net.InterfaceAddrs()` do not work correctly on newer Android versions. This tool uses https://github.com/wlynxg/anet to resolve this issue. In Android, `anet` has to be run/built with `-ldflags "-checklinkname=0"` to avoid this error: `link: github.com/wlynxg/anet: invalid reference to net.zoneCache`.

Additionally, a small patch is applied to enable TLS certificate requests, which are currently disabled in the Tailscale library. This can also be set up by go.work (this is useful when ts-proxy is used as library).

## Can't connect to itself (#18829)
The latest Tailscale release has a bug ( https://github.com/tailscale/tailscale/issues/18829 ), where tsnet can't connect to itself. It's already fixed but not released. This is fixed in ts-proxy binary release. See GitHub Actions configuration for detail.

# TODO
- HTTP Proxy support
- SOCKS5 authentication
