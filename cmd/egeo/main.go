// Command egeo builds the merged IP geolocation databases.
//
//	egeo fetch    download upstream files into the cache and write inputs.json
//	egeo changed  compare inputs.json with the previous release's manifest.json
//	egeo build    build the databases, manifest.json, ACCURACY.md and reports
//	egeo verify   check the built databases
//	egeo all      fetch + build + verify (for local runs)
//	egeo gen-cn-admin  generate cn_admin / cn_cities candidates from GeoNames
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"time"

	"github.com/jinkela1145/enhanced-geoip/internal/config"
	"github.com/jinkela1145/enhanced-geoip/internal/pipeline"
	"github.com/jinkela1145/enhanced-geoip/internal/verify"
)

type common struct {
	config, cache, data, out, inputs, known, prev, version string
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: egeo <fetch|changed|build|verify|all|gen-cn-admin> [flags]")
	os.Exit(2)
}

func main() {
	log.SetFlags(log.Ltime | log.Lmsgprefix)
	log.SetPrefix("egeo: ")
	if len(os.Args) < 2 {
		usage()
	}
	cmd := os.Args[1]
	fs := flag.NewFlagSet(cmd, flag.ExitOnError)
	var c common
	fs.StringVar(&c.config, "config", "config.json", "config file")
	fs.StringVar(&c.cache, "cache", ".cache", "download cache directory")
	fs.StringVar(&c.data, "data", "data", "directory with the curated CSV tables")
	fs.StringVar(&c.out, "out", "dist", "output directory")
	fs.StringVar(&c.inputs, "inputs", "", "inputs.json written by fetch (default <out>/inputs.json)")
	fs.StringVar(&c.known, "known", "testdata/known_ips.csv", "known address list for verify (empty to skip)")
	fs.StringVar(&c.prev, "prev", "prev/manifest.json", "previous release's manifest.json for changed")
	fs.StringVar(&c.version, "version", "", "release version (default: today's UTC date, YYYY.MM.DD)")
	_ = fs.Parse(os.Args[2:])
	if c.inputs == "" {
		c.inputs = c.out + "/inputs.json"
	}
	cfg, err := config.Load(c.config)
	if err != nil {
		log.Fatal(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	start := time.Now()
	switch cmd {
	case "fetch":
		runFetch(ctx, cfg, c)
	case "changed":
		in, err := pipeline.ReadInputs(c.inputs)
		if err != nil {
			log.Fatal(err)
		}
		changed, why := pipeline.Changed(c.prev, in)
		log.Print(why)
		fmt.Println(changed)
	case "build":
		runBuild(cfg, c)
	case "verify":
		runVerify(cfg, c)
	case "all":
		runFetch(ctx, cfg, c)
		runBuild(cfg, c)
		runVerify(cfg, c)
	case "gen-cn-admin":
		if err := pipeline.GenCNAdmin(ctx, cfg, c.cache, c.out+"/reports", log.Printf); err != nil {
			log.Fatal(err)
		}
	default:
		usage()
	}
	log.Printf("%s done in %s", cmd, time.Since(start).Round(time.Second))
}

func runFetch(ctx context.Context, cfg *config.Config, c common) {
	in, err := pipeline.Fetch(ctx, pipeline.FetchOptions{Config: cfg, CacheDir: c.cache, DataDir: c.data, Logf: log.Printf}, c.inputs)
	if err != nil {
		log.Fatal(err)
	}
	for _, w := range in.Warnings {
		log.Printf("warning: %s", w)
	}
	log.Printf("fetched %d sources, fingerprint %s", len(in.Sources), in.Fingerprint)
}

func runBuild(cfg *config.Config, c common) {
	in, err := pipeline.ReadInputs(c.inputs)
	if err != nil {
		log.Fatal(err)
	}
	m, err := pipeline.Build(pipeline.BuildOptions{Config: cfg, Inputs: in, CacheDir: c.cache, DataDir: c.data,
		OutDir: c.out, Version: c.version, Logf: log.Printf})
	if err != nil {
		log.Fatal(err)
	}
	for name, o := range m.Outputs {
		log.Printf("%s: %s %.1f MB, %d ranges", name, o.File, float64(o.Size)/(1<<20), o.Ranges)
	}
	for _, w := range m.Warnings {
		log.Printf("warning: %s", w)
	}
}

func runVerify(cfg *config.Config, c common) {
	rep, err := verify.Run(cfg, c.out, c.known)
	if rep != nil {
		log.Printf("verified %d full networks, %d lite networks, %d known addresses", rep.FullNetworks, rep.LiteNetworks, rep.KnownChecked)
	}
	if err != nil {
		log.Fatalf("verification failed:\n%v", err)
	}
}
