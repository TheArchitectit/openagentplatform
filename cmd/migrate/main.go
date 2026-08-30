// Command migrate applies or inspects the platform schema from the command
// line. It is a thin face over internal/db — the same embedded migration set
// (internal/db/migrations) and the same runner the server uses at boot, so
// CLI and boot can never drift.
//
// Usage:
//
//	go run ./cmd/migrate up              # apply all pending migrations
//	go run ./cmd/migrate status          # current vs newest available version
//	go run ./cmd/migrate force <n>       # mark ledger at <n> without running it
//
// The DSN comes from -dsn, $POSTGRES_DSN, or $DATABASE_URL. Ordinary beta
// deployments never need this — the server auto-migrates at boot
// (OAP_AUTO_MIGRATE=true). Use it when OAP_AUTO_MIGRATE=false and a human or
// CI owns schema rollout, or to diagnose a dirty ledger.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/openagentplatform/openagentplatform/internal/db"
)

func main() {
	dsnFlag := flag.String("dsn", "", "Postgres DSN (defaults to $POSTGRES_DSN, then $DATABASE_URL)")
	flag.Parse()
	args := flag.Args()

	dsn := strings.TrimSpace(*dsnFlag)
	if dsn == "" {
		dsn = strings.TrimSpace(os.Getenv("POSTGRES_DSN"))
	}
	if dsn == "" {
		dsn = strings.TrimSpace(os.Getenv("DATABASE_URL"))
	}
	if dsn == "" {
		fmt.Fprintln(os.Stderr, "migrate: no DSN; pass -dsn or set POSTGRES_DSN")
		os.Exit(2)
	}
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: migrate [-dsn DSN] up|status|force <version>")
		os.Exit(2)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))

	var err error
	switch args[0] {
	case "up":
		err = db.Migrate(ctx, dsn, log)
	case "status":
		err = runStatus(ctx, dsn)
	case "force":
		if len(args) != 2 {
			fmt.Fprintln(os.Stderr, "force needs a version: migrate force <n>")
			os.Exit(2)
		}
		var n int
		n, err = strconv.Atoi(args[1])
		if err != nil {
			fmt.Fprintf(os.Stderr, "migrate: bad version %q: %v\n", args[1], err)
			os.Exit(2)
		}
		if err = db.ForceVersion(ctx, dsn, n); err == nil {
			fmt.Printf("forced schema_migrations to version %d\n", n)
		}
	default:
		fmt.Fprintln(os.Stderr, "usage: migrate [-dsn DSN] up|status|force <version>")
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "migrate %s: %v\n", args[0], err)
		os.Exit(1)
	}
}

func runStatus(ctx context.Context, dsn string) error {
	version, dirty, maxSource, err := db.MigrationStatus(ctx, dsn)
	if err != nil {
		return err
	}
	state := "clean"
	if dirty {
		state = "DIRTY (a migration failed halfway; fix and re-run, or force)"
	}
	fmt.Printf("database version: %d (%s)\n", version, state)
	fmt.Printf("embedded source newest version: %d\n", maxSource)
	if !dirty && version < maxSource {
		fmt.Printf("%d migration(s) pending — run: migrate up\n", maxSource-version)
	} else if version == maxSource {
		fmt.Println("up to date")
	}
	return nil
}
