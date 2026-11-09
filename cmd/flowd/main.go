package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Songxwn/flowd/internal/api"
	"github.com/Songxwn/flowd/internal/capture"
	"github.com/Songxwn/flowd/internal/config"
	"github.com/Songxwn/flowd/internal/model"
	"github.com/Songxwn/flowd/internal/vlogs"
)

var version = "dev"

func main() {
	configPath := flag.String("config", "", "配置文件路径，也可用环境变量 FLOWD_CONFIG")
	showVersion := flag.Bool("version", false, "打印版本后退出")
	flag.Parse()
	if *showVersion {
		fmt.Println(version)
		return
	}
	path := *configPath
	if path == "" {
		path = os.Getenv("FLOWD_CONFIG")
	}
	if path == "" {
		path = "config.yaml"
	}
	cfg, err := config.Load(path)
	if err != nil {
		log.Fatalf("读取配置失败: %v", err)
	}
	if cfg.Hostname == "" {
		host, err := os.Hostname()
		if err != nil || host == "" {
			host = "localhost"
		}
		cfg.Hostname = host
	}
	if cfg.Token == "" {
		token, err := config.RandomToken()
		if err != nil {
			log.Fatalf("生成网页口令失败: %v", err)
		}
		cfg.Token = token
		if err := config.Save(path, cfg); err != nil {
			log.Printf("网页口令没能写入 %s: %v", path, err)
		}
		log.Printf("网页尚未设置口令，已生成并用于本次登录: %s", token)
	}

	log.Printf("flowd %s 配置 %s，监听 %s，VictoriaLogs %s 租户 %s", version, path, cfg.Listen, cfg.VictoriaLogs.URL, cfg.VictoriaLogs.ID)
	if !capture.Supported() {
		log.Printf("当前系统不能抓包，只能查询已经写入 VictoriaLogs 的日志")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	out := make(chan model.Record, 2048)
	client := vlogs.New(cfg.VictoriaLogs.URL, string(cfg.VictoriaLogs.ID), version)
	sink := vlogs.NewSink(client, out)
	go sink.Run()

	svc := capture.New(out, cfg.Hostname)
	svc.Start(ctx, cfg)

	web := api.New(api.Options{
		Path:    path,
		Bound:   cfg.Listen,
		Version: version,
		Config:  cfg,
		Service: svc,
		Logs:    client,
		Sink:    sink,
	})
	go web.Watch(ctx.Done())

	httpSrv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           web.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() {
		errCh <- httpSrv.ListenAndServe()
	}()
	select {
	case <-ctx.Done():
		log.Printf("正在停止")
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("网页服务退出: %v", err)
		}
		stop()
	}

	shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	_ = httpSrv.Shutdown(shutCtx)
	cancel()
	svc.Close()
	close(out)
	sink.Wait()
}
