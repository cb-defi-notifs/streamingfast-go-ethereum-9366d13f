// Copyright 2016 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package debug

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	_ "net/http/pprof"
	"os"
	"runtime"

	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/firehose"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/metrics"
	"github.com/ethereum/go-ethereum/metrics/exp"
	"github.com/ethereum/go-ethereum/params"
	"github.com/fjl/memsize/memsizeui"
	colorable "github.com/mattn/go-colorable"
	"github.com/mattn/go-isatty"
	"gopkg.in/urfave/cli.v1"
)

var Memsize memsizeui.Handler

var (
	verbosityFlag = cli.IntFlag{
		Name:  "verbosity",
		Usage: "Logging verbosity: 0=silent, 1=error, 2=warn, 3=info, 4=debug, 5=detail",
		Value: 3,
	}
	vmoduleFlag = cli.StringFlag{
		Name:  "vmodule",
		Usage: "Per-module verbosity: comma-separated list of <pattern>=<level> (e.g. eth/*=5,p2p=4)",
		Value: "",
	}
	backtraceAtFlag = cli.StringFlag{
		Name:  "backtrace",
		Usage: "Request a stack trace at a specific logging statement (e.g. \"block.go:271\")",
		Value: "",
	}
	debugFlag = cli.BoolFlag{
		Name:  "debug",
		Usage: "Prepends log messages with call-site location (file and line number)",
	}
	pprofFlag = cli.BoolFlag{
		Name:  "pprof",
		Usage: "Enable the pprof HTTP server",
	}
	pprofPortFlag = cli.IntFlag{
		Name:  "pprofport",
		Usage: "pprof HTTP server listening port",
		Value: 6060,
	}
	pprofAddrFlag = cli.StringFlag{
		Name:  "pprofaddr",
		Usage: "pprof HTTP server listening interface",
		Value: "127.0.0.1",
	}
	memprofilerateFlag = cli.IntFlag{
		Name:  "memprofilerate",
		Usage: "Turn on memory profiling with the given rate",
		Value: runtime.MemProfileRate,
	}
	blockprofilerateFlag = cli.IntFlag{
		Name:  "blockprofilerate",
		Usage: "Turn on block profiling with the given rate",
	}
	cpuprofileFlag = cli.StringFlag{
		Name:  "cpuprofile",
		Usage: "Write CPU profile to the given file",
	}
	traceFlag = cli.StringFlag{
		Name:  "trace",
		Usage: "Write execution trace to the given file",
	}

	// Firehose Flags
	firehoseEnabledFlag = cli.BoolFlag{
		Name:  "firehose-enabled",
		Usage: "Activate/deactivate Firehose instrumentation, disabled by default",
	}
	firehoseSyncInstrumentationFlag = cli.BoolTFlag{
		Name:  "firehose-sync-instrumentation",
		Usage: "Activate/deactivate Firehose sync output instrumentation, enabled by default",
	}
	firehoseMiningEnabledFlag = cli.BoolFlag{
		Name:  "firehose-mining-enabled",
		Usage: "Activate/deactivate mining code even if Firehose is active, required speculative execution on local miner node, disabled by default",
	}
	firehoseBlockProgressFlag = cli.BoolFlag{
		Name:  "firehose-block-progress",
		Usage: "Activate/deactivate Firehose block progress output instrumentation, disabled by default",
	}
	firehoseGenesisFileFlag = cli.StringFlag{
		Name:  "firehose-genesis-file",
		Usage: "On private chains where the genesis config is not known to Geth, you **must** provide the 'genesis.json' file path for proper instrumentation of genesis block",
		Value: "",
	}
)

// Flags holds all command-line flags required for debugging.
var Flags = []cli.Flag{
	verbosityFlag, vmoduleFlag, backtraceAtFlag, debugFlag,
	pprofFlag, pprofAddrFlag, pprofPortFlag,
	memprofilerateFlag, blockprofilerateFlag, cpuprofileFlag, traceFlag,
}

// FirehoseFlags holds all StreamingFast Firehose related command-line flags.
var FirehoseFlags = []cli.Flag{
	firehoseEnabledFlag, firehoseSyncInstrumentationFlag, firehoseMiningEnabledFlag, firehoseBlockProgressFlag,
	firehoseGenesisFileFlag,
}

var (
	ostream log.Handler
	glogger *log.GlogHandler
)

func init() {
	usecolor := (isatty.IsTerminal(os.Stderr.Fd()) || isatty.IsCygwinTerminal(os.Stderr.Fd())) && os.Getenv("TERM") != "dumb"
	output := io.Writer(os.Stderr)
	if usecolor {
		output = colorable.NewColorableStderr()
	}
	ostream = log.StreamHandler(output, log.TerminalFormat(usecolor))
	glogger = log.NewGlogHandler(ostream)
}

// Setup initializes profiling and logging based on the CLI flags.
// It should be called as early as possible in the program.
func Setup(ctx *cli.Context, logdir string, genesis *core.Genesis) error {
	// logging
	log.PrintOrigins(ctx.GlobalBool(debugFlag.Name))
	if logdir != "" {
		rfh, err := log.RotatingFileHandler(
			logdir,
			262144,
			log.JSONFormatOrderedEx(false, true),
		)
		if err != nil {
			return err
		}
		glogger.SetHandler(log.MultiHandler(ostream, rfh))
	}
	glogger.Verbosity(log.Lvl(ctx.GlobalInt(verbosityFlag.Name)))
	glogger.Vmodule(ctx.GlobalString(vmoduleFlag.Name))
	glogger.BacktraceAt(ctx.GlobalString(backtraceAtFlag.Name))
	log.Root().SetHandler(glogger)

	// profiling, tracing
	runtime.MemProfileRate = ctx.GlobalInt(memprofilerateFlag.Name)
	Handler.SetBlockProfileRate(ctx.GlobalInt(blockprofilerateFlag.Name))
	if traceFile := ctx.GlobalString(traceFlag.Name); traceFile != "" {
		if err := Handler.StartGoTrace(traceFile); err != nil {
			return err
		}
	}
	if cpuFile := ctx.GlobalString(cpuprofileFlag.Name); cpuFile != "" {
		if err := Handler.StartCPUProfile(cpuFile); err != nil {
			return err
		}
	}

	// pprof server
	if ctx.GlobalBool(pprofFlag.Name) {
		address := fmt.Sprintf("%s:%d", ctx.GlobalString(pprofAddrFlag.Name), ctx.GlobalInt(pprofPortFlag.Name))
		StartPProf(address)
	}

	// Firehose
	log.Info("Initializing firehose")
	firehose.Enabled = ctx.GlobalBool(firehoseEnabledFlag.Name)
	firehose.SyncInstrumentationEnabled = ctx.GlobalBoolT(firehoseSyncInstrumentationFlag.Name)
	firehose.MiningEnabled = ctx.GlobalBool(firehoseMiningEnabledFlag.Name)
	firehose.BlockProgressEnabled = ctx.GlobalBool(firehoseBlockProgressFlag.Name)

	genesisProvenance := "unset"

	if genesis != nil {
		firehose.GenesisConfig = genesis
		genesisProvenance = "Geth Specific Flag"
	} else {
		if genesisFilePath := ctx.GlobalString(firehoseGenesisFileFlag.Name); genesisFilePath != "" {
			file, err := os.Open(genesisFilePath)
			if err != nil {
				return fmt.Errorf("firehose open genesis file: %w", err)
			}
			defer file.Close()

			genesis := &core.Genesis{}
			if err := json.NewDecoder(file).Decode(genesis); err != nil {
				return fmt.Errorf("decode genesis file %q: %w", genesisFilePath, err)
			}

			firehose.GenesisConfig = genesis
			genesisProvenance = "Flag " + firehoseGenesisFileFlag.Name
		} else {
			firehose.GenesisConfig = core.DefaultGenesisBlock()
			genesisProvenance = "Geth Default"
		}
	}

	log.Info("Firehose initialized",
		"enabled", firehose.Enabled,
		"sync_instrumentation_enabled", firehose.SyncInstrumentationEnabled,
		"mining_enabled", firehose.MiningEnabled,
		"block_progress_enabled", firehose.BlockProgressEnabled,
		"genesis_provenance", genesisProvenance,
		"firehose_version", params.FirehoseVersion(),
		"geth_version", params.VersionWithMeta,
		"chain_variant", params.Variant,
	)

	return nil
}

func StartPProf(address string) {
	// Hook go-metrics into expvar on any /debug/metrics request, load all vars
	// from the registry into expvar, and execute regular expvar handler.
	exp.Exp(metrics.DefaultRegistry)
	http.Handle("/memsize/", http.StripPrefix("/memsize", &Memsize))
	log.Info("Starting pprof server", "addr", fmt.Sprintf("http://%s/debug/pprof", address))
	go func() {
		if err := http.ListenAndServe(address, nil); err != nil {
			log.Error("Failure in running pprof server", "err", err)
		}
	}()
}

// Exit stops all running profiles, flushing their output to the
// respective file.
func Exit() {
	Handler.StopCPUProfile()
	Handler.StopGoTrace()
}
