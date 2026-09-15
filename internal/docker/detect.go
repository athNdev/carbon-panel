package docker

import (
	"context"
	"net"
	"os"
	"sort"
	"strconv"
	"sync"
	"time"
)

// DockerCandidate is a single auto-detect probe result for a Docker daemon
// endpoint reachable on one of the host's network interfaces.
type DockerCandidate struct {
	Host      string `json:"host"`
	Port      int    `json:"port"`
	Reachable bool   `json:"reachable"`
	LatencyMs int64  `json:"latencyMs"`
	Source    string `json:"source"`
}

// dockerProbePorts are the conventional plain-text and TLS Docker daemon ports.
var dockerProbePorts = []int{2375, 2376}

const dockerProbeTimeout = 400 * time.Millisecond

// DetectDockerDaemons probes the local unix socket plus TCP 2375/2376 on
// every up network interface address (and its likely gateway) to find active
// Docker daemons. Probes run concurrently and are bounded by ctx; unreachable
// endpoints are still returned with Reachable=false so the UI can show what
// was scanned.
func DetectDockerDaemons(ctx context.Context) []DockerCandidate {
	type target struct {
		host   string
		port   int
		source string
	}

	seen := make(map[string]string)
	var targets []target
	add := func(host string, port int, source string) {
		key := host + ":" + strconv.Itoa(port)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = source
		targets = append(targets, target{host: host, port: port, source: source})
	}

	// Always probe loopback.
	for _, port := range dockerProbePorts {
		add("127.0.0.1", port, "loopback")
	}

	// Probe every up, non-loopback interface address plus a gateway guess.
	if ifaces, err := net.Interfaces(); err == nil {
		for _, iface := range ifaces {
			if iface.Flags&net.FlagUp == 0 {
				continue
			}
			addrs, err := iface.Addrs()
			if err != nil {
				continue
			}
			for _, a := range addrs {
				ip := extractIPv4(a)
				if ip == nil || ip.IsLoopback() {
					continue
				}
				for _, port := range dockerProbePorts {
					add(ip.String(), port, "interface:"+iface.Name)
				}
				if gw := gatewayGuess(ip); gw != nil && !gw.Equal(ip) {
					for _, port := range dockerProbePorts {
						add(gw.String(), port, "gateway-guess")
					}
				}
			}
		}
	}

	candidates := make([]DockerCandidate, 0, len(targets)+1)

	// Local unix socket candidate (existence check, no dial needed).
	const sockPath = "/var/run/docker.sock"
	sockReachable := false
	if st, err := os.Stat(sockPath); err == nil && !st.IsDir() {
		sockReachable = true
	}
	candidates = append(candidates, DockerCandidate{
		Host:      "unix:///var/run/docker.sock",
		Port:      0,
		Reachable: sockReachable,
		LatencyMs: 0,
		Source:    "local-socket",
	})

	// Concurrent TCP probes with bounded parallelism.
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 32)
	for _, t := range targets {
		if ctx.Err() != nil {
			break
		}
		wg.Add(1)
		go func(t target) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				return
			}
			start := time.Now()
			d := &net.Dialer{Timeout: dockerProbeTimeout}
			conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort(t.host, strconv.Itoa(t.port)))
			latency := time.Since(start).Milliseconds()
			c := DockerCandidate{
				Host:      "tcp://" + net.JoinHostPort(t.host, strconv.Itoa(t.port)),
				Port:      t.port,
				Reachable: err == nil,
				LatencyMs: latency,
				Source:    t.source,
			}
			if conn != nil {
				_ = conn.Close()
			}
			mu.Lock()
			candidates = append(candidates, c)
			mu.Unlock()
		}(t)
	}
	wg.Wait()

	// Reachable endpoints first, then alphabetical for stable output.
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Reachable != candidates[j].Reachable {
			return candidates[i].Reachable
		}
		return candidates[i].Host < candidates[j].Host
	})

	return candidates
}

// extractIPv4 pulls the IPv4 address out of a net.Addr, if any.
func extractIPv4(a net.Addr) net.IP {
	switch v := a.(type) {
	case *net.IPNet:
		return v.IP.To4()
	case *net.IPAddr:
		return v.IP.To4()
	}
	return nil
}

// gatewayGuess returns the likely gateway (.1) for an IPv4 host address.
func gatewayGuess(ip net.IP) net.IP {
	v4 := ip.To4()
	if v4 == nil {
		return nil
	}
	gw := make(net.IP, net.IPv4len)
	copy(gw, v4)
	gw[3] = 1
	return gw
}
