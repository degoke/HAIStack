package command

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/degoke/health-ai-stack/cmd/haistack/internal/app"
	"github.com/spf13/cobra"
)

func newServeCommand(opts *Options, printer *app.Printer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the managed HTTP runtime server",
		Long: `Build and start a Health AI Stack runtime using haistack.yaml. The server blocks
until interrupted and prints the bound listen address on startup.`,
		Example: `  haistack serve
  haistack serve --http-addr 127.0.0.1:9090`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := opts.loadConfig()
			if err != nil {
				return exitErr(printer, err)
			}
			addr := cfg.Runtime.HTTPAddr
			if opts.HTTPAddr != "" {
				addr = opts.HTTPAddr
			}
			ctx := context.Background()
			rt, err := app.BuildRuntime(ctx, cfg, addr)
			if err != nil {
				return exitErr(printer, err)
			}
			defer func() { _ = rt.Shutdown(context.Background()) }()

			if err := rt.Start(ctx); err != nil {
				return exitErr(printer, err)
			}

			startMsg := map[string]any{
				"mode":    string(rt.Mode()),
				"address": rt.HTTPAddr().String(),
				"search":  cfg.Runtime.EnableSearch,
			}
			if printer.Format == app.OutputJSON {
				_ = printer.Print(startMsg)
			} else {
				writeStdout(printer, fmt.Sprintf("listening on http://%s (mode=%s, search=%v)",
					rt.HTTPAddr().String(), rt.Mode(), cfg.Runtime.EnableSearch))
			}

			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
			<-sigCh
			signal.Stop(sigCh)
			return rt.Shutdown(context.Background())
		},
	}
	return cmd
}
