package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/Juniper/go-netconf/netconf"
	"golang.org/x/crypto/ssh"
)

func main() {
	host := flag.String("host", "localhost", "NETCONF server host")
	port := flag.Int("port", 830, "NETCONF server port")
	user := flag.String("user", "", "SSH username")
	password := flag.String("password", "", "SSH password")
	timeout := flag.Duration("timeout", 10*time.Second, "NETCONF operation timeout")
	debug := flag.Bool("debug", false, "enable debug logging")
	flag.Parse()

	if *user == "" {
		fmt.Fprintln(os.Stderr, "error: --user is required")
		os.Exit(1)
	}

	if *debug || os.Getenv("NETCONF_DEBUG") == "1" {
		netconf.SetDebugLogger(os.Stderr)
	}

	target := fmt.Sprintf("%s:%d", *host, *port)
	config := &ssh.ClientConfig{
		User: *user,
		Auth: []ssh.AuthMethod{
			ssh.Password(*password),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}

	fmt.Printf("[INFO] Connecting to %s (timeout=%s)\n", target, *timeout)
	s, err := netconf.DialSSHTimeout(target, config, *timeout)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[ERROR] Connection failed: %v\n", err)
		os.Exit(1)
	}
	defer s.Close()

	fmt.Printf("[INFO] Connected (session-id=%d)\n", s.SessionID)

	reply, err := s.Exec(netconf.RawMethod("<get/>"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "[ERROR] RPC failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("[INFO] RPC succeeded\n")
	fmt.Printf("[DATA] %s\n", reply.RawReply)
}
