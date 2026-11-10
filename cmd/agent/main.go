package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"

	"github.com/xingshanghe/x-phone/internal/agentrt"
)

func main() {
	server := flag.String("server", "http://127.0.0.1:18110", "control plane base URL")
	token := flag.String("token", os.Getenv("XPHONE_AGENT_TOKEN"), "host agent token")
	simulate := flag.Bool("simulate", false, "complete tasks without Docker")
	python := flag.String("python", os.Getenv("XPHONE_PYTHON"), "python executable")
	pythonPath := flag.String("pythonpath", "tools/phone", "PYTHONPATH for the phone package")
	flag.Parse()
	if *token == "" {
		log.Fatal("agent token is required")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	client := &agentrt.Client{Options: agentrt.Options{
		Server: *server, Token: *token, Simulate: *simulate, Python: *python, PythonPath: *pythonPath,
	}}
	if err := client.Loop(ctx); err != nil && ctx.Err() == nil {
		log.Fatal(err)
	}
}
