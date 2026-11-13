package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/larryjoelane/healbot/internal/app"
	"github.com/larryjoelane/healbot/internal/config"
	"github.com/larryjoelane/healbot/internal/demo"
	"github.com/larryjoelane/healbot/internal/model"
	"github.com/larryjoelane/healbot/internal/pipeline"
)

func main() {
	hold := flag.Bool("hold", false, "keep the console running after the scenario")
	addr := flag.String("addr", "127.0.0.1:43117", "address to bind")
	flag.Parse()

	cfg := config.Demo()
	cfg.Server.Addr = *addr
	cfg.Audit.Path = ""
	application, err := app.Build(cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		if err := application.Run(ctx); err != nil {
			fmt.Fprintln(os.Stderr, err)
		}
	}()
	base := "http://" + *addr
	if err := wait(base); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := run(base, cfg.Server.WebhookToken); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("\nConsole: %s\n", base)
	if *hold {
		fmt.Println("Holding the process. Ctrl-C stops it.")
		select {}
	}
	cancel()
}

func wait(base string) error {
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		res, err := http.Get(base + "/healthz")
		if err == nil {
			res.Body.Close()
			if res.StatusCode == 200 {
				return nil
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("server did not become ready")
}

func run(base, token string) error {
	req, err := http.NewRequest(http.MethodPost, base+"/v1/ingest/monitor?sync=1", bytes.NewReader(demo.MonitorBody()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Healbot-Token", token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("ingest status %d: %s", res.StatusCode, body)
	}
	var result pipeline.Result
	if err := json.Unmarshal(body, &result); err != nil {
		return err
	}
	inc := result.Incident
	fmt.Println("Healbot demo")
	fmt.Println("------------")
	fmt.Printf("incident: %s\n", inc.ID)
	if inc.Hypothesis != nil {
		fmt.Printf("tier 1: %s (%.0f%%, %s)\n", inc.Hypothesis.Summary, inc.Hypothesis.Confidence*100, inc.Hypothesis.Category)
	}
	if len(inc.Actions) == 0 {
		return fmt.Errorf("tier 2 did not plan an action")
	}
	act := inc.Actions[0]
	fmt.Printf("tier 2: %s %s — %s\n", act.Kind, act.Outcome, act.Detail)
	if act.Outcome != "dry_run" {
		return fmt.Errorf("expected a dry-run, got %s", act.Outcome)
	}
	calls, err := http.Get(base + "/v1/debug/calls")
	if err != nil {
		return err
	}
	defer calls.Body.Close()
	raw, _ := io.ReadAll(calls.Body)
	if string(bytes.TrimSpace(raw)) != "[]" && string(bytes.TrimSpace(raw)) != "null" {
		return fmt.Errorf("dry-run invoked the runtime: %s", raw)
	}
	if inc.PullRequest == nil {
		return fmt.Errorf("tier 3 did not open a pull request")
	}
	fmt.Printf("tier 3: %s %s (%s)\n", inc.PullRequest.ID, inc.PullRequest.Title, inc.PullRequest.URL)
	fmt.Println("pull requests are not merged")
	if inc.Status != model.StatusPROpened {
		return fmt.Errorf("status %s", inc.Status)
	}
	fmt.Println("OK")
	return nil
}
