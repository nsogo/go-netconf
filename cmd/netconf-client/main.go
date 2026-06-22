package main

import (
	"flag"
	"fmt"
	"io"
	stdlog "log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/Juniper/go-netconf/netconf"
	"golang.org/x/crypto/ssh"
)

// multiFlag allows -rpc to be specified multiple times.
type multiFlag []string

func (m *multiFlag) String() string { return strings.Join(*m, ", ") }
func (m *multiFlag) Set(v string) error {
	*m = append(*m, v)
	return nil
}

type rpcResult struct {
	index int
	rpc   string
	reply *netconf.RPCReply
	err   error
}

func execRPC(index int, rpcXML string, target string, config *ssh.ClientConfig) rpcResult {
	s, err := netconf.DialSSH(target, config)
	if err != nil {
		return rpcResult{index: index, rpc: rpcXML, err: fmt.Errorf("connection failed: %w", err)}
	}
	defer s.Close()

	reply, err := s.Exec(netconf.RawMethod(rpcXML))
	return rpcResult{index: index, rpc: rpcXML, reply: reply, err: err}
}

func main() {
	host := flag.String("host", "localhost", "NETCONF server host")
	port := flag.Int("port", 830, "NETCONF server port")
	user := flag.String("user", "", "SSH username")
	password := flag.String("password", "", "SSH password")
	timeout := flag.Duration("timeout", 10*time.Second, "NETCONF operation timeout")
	debug := flag.Bool("debug", false, "enable debug logging (REQUEST/REPLY)")

	var rpcs multiFlag
	flag.Var(&rpcs, "rpc", "NETCONF RPC XML to execute (can be specified multiple times)")
	flag.Parse()

	// Always write output to a timestamped log file in addition to stdout/stderr.
	jst := time.FixedZone("JST", 9*60*60)
	logFileName := time.Now().In(jst).Format("netconf_2006_01_02_15_04_05.log")
	f, err := os.OpenFile(logFileName, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: cannot open log file: %v\n", err)
		os.Exit(1)
	}
	defer f.Close()
	logOut := io.MultiWriter(os.Stdout, f)
	logErr := io.MultiWriter(os.Stderr, f)
	fmt.Fprintf(logOut, "[INFO] Log file: %s\n", logFileName)

	if *debug || os.Getenv("NETCONF_DEBUG") == "1" {
		netconf.SetLog(netconf.NewStdLog(
			stdlog.New(logErr, "[NETCONF DEBUG] ", 0),
			netconf.LogDebug,
		))
	}

	if *user == "" {
		fmt.Fprintln(os.Stderr, "error: --user is required")
		os.Exit(1)
	}

	if len(rpcs) == 0 {
		rpcs = multiFlag{"<get-vrrp-information><summary/></get-vrrp-information>"}
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

	fmt.Fprintf(logOut, "[INFO] Connecting to %s — %d RPC(s) in parallel (timeout=%s)\n", target, len(rpcs), *timeout)

	resultsCh := make(chan rpcResult, len(rpcs))
	var wg sync.WaitGroup

	for i, rpcXML := range rpcs {
		wg.Add(1)
		i, rpcXML := i, rpcXML
		go func() {
			defer wg.Done()
			resultsCh <- execRPC(i, rpcXML, target, config)
		}()
	}

	go func() {
		wg.Wait()
		close(resultsCh)
	}()

	// Collect results with timeout.
	results := make([]rpcResult, len(rpcs))
	timeoutCh := time.After(*timeout)
	collected := 0
	exitCode := 0

loop:
	for collected < len(rpcs) {
		select {
		case <-timeoutCh:
			fmt.Fprintf(logErr, "[ERROR] Timeout after %s (collected %d/%d results)\n", *timeout, collected, len(rpcs))
			exitCode = 1
			break loop
		case res := <-resultsCh:
			results[res.index] = res
			collected++
		}
	}

	// Print results in the original order.
	for _, res := range results {
		if res.rpc == "" {
			continue // not collected due to timeout
		}
		label := fmt.Sprintf("RPC[%d]", res.index)
		if res.err != nil {
			fmt.Fprintf(logErr, "[ERROR] %s failed: %v\n", label, res.err)
			exitCode = 1
			continue
		}
		fmt.Fprintf(logOut, "[INFO] %s succeeded\n", label)
		fmt.Fprintf(logOut, "[DATA] %s\n", res.reply.RawReply)
	}

	os.Exit(exitCode)
}
