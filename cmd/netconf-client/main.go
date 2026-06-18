package main

import (
	"flag"
	"fmt"
	stdlog "log"
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
	debug := flag.Bool("debug", false, "enable debug logging (REQUEST/REPLY)")
	rpc := flag.String("rpc", "<get-vrrp-information><summary/></get-vrrp-information>", "NETCONF RPC XML to execute")
	flag.Parse()

	if *debug || os.Getenv("NETCONF_DEBUG") == "1" {
		netconf.SetLog(netconf.NewStdLog(
			stdlog.New(os.Stderr, "[NETCONF DEBUG] ", 0),
			netconf.LogDebug,
		))
	}

	if *user == "" {
		fmt.Fprintln(os.Stderr, "error: --user is required")
		os.Exit(1)
	}

	target := fmt.Sprintf("%s:%d", *host, *port)
	config := &ssh.ClientConfig{
		User: *user,
		Auth: []ssh.AuthMethod{
			ssh.Password(*password),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         10 * time.Second,
	}

	fmt.Printf("[INFO] Connecting to %s (timeout=%s)\n", target, *timeout)
	s, err := netconf.DialSSH(target, config)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[ERROR] Connection failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("[INFO] Connected (session-id=%d)\n", s.SessionID)

	timeoutCh := time.After(*timeout)
	done := make(chan struct{})
	var reply *netconf.RPCReply
	var rpcErr error
	go func() {
		reply, rpcErr = s.Exec(netconf.RawMethod(*rpc))
		close(done)
	}()

	select {
	case <-timeoutCh:
		s.Close()
		fmt.Fprintf(os.Stderr, "[ERROR] Timeout after %s waiting for RPC reply\n", *timeout)
		os.Exit(1)
	case <-done:
	}

	if rpcErr != nil {
		s.Close()
		fmt.Fprintf(os.Stderr, "[ERROR] RPC failed: %v\n", rpcErr)
		os.Exit(1)
	}
	defer s.Close()

	fmt.Printf("[INFO] RPC succeeded\n")
	fmt.Printf("[DATA] %s\n", reply.RawReply)
}
