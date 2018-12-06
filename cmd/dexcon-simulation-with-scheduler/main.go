// Copyright 2018 The dexon-consensus Authors
// This file is part of the dexon-consensus library.
//
// The dexon-consensus library is free software: you can redistribute it
// and/or modify it under the terms of the GNU Lesser General Public License as
// published by the Free Software Foundation, either version 3 of the License,
// or (at your option) any later version.
//
// The dexon-consensus library is distributed in the hope that it will be
// useful, but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the GNU Lesser
// General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the dexon-consensus library. If not, see
// <http://www.gnu.org/licenses/>.

package main

import (
	"flag"
	"log"
	"math"
	"math/rand"
	"net/http"
	_ "net/http/pprof"
	"os"
	"runtime"
	"runtime/pprof"
	"time"

	"github.com/dexon-foundation/dexon-consensus/common"
	"github.com/dexon-foundation/dexon-consensus/core"
	"github.com/dexon-foundation/dexon-consensus/core/test"
	integration "github.com/dexon-foundation/dexon-consensus/integration_test"
	"github.com/dexon-foundation/dexon-consensus/simulation/config"
)

var (
	configFile = flag.String("config", "", "path to simulation config file")
	cpuprofile = flag.String("cpuprofile", "", "write cpu profile to `file`")
	memprofile = flag.String("memprofile", "", "write memory profile to `file`")
)

func main() {
	flag.Parse()
	rand.Seed(time.Now().UnixNano())
	// Supports runtime pprof monitoring.
	go func() {
		log.Println(http.ListenAndServe("localhost:6060", nil))
	}()
	if *configFile == "" {
		log.Fatal("error: no configuration file specified")
	}
	if *cpuprofile != "" {
		f, err := os.Create(*cpuprofile)
		if err != nil {
			log.Fatal("could not create CPU profile: ", err)
		}
		if err := pprof.StartCPUProfile(f); err != nil {
			log.Fatal("could not start CPU profile: ", err)
		}
		defer pprof.StopCPUProfile()
	}

	cfg, err := config.Read(*configFile)
	if err != nil {
		log.Fatal("unable to read config: ", err)
	}
	// Setup latencies, nodes.
	networkLatency := &test.NormalLatencyModel{
		Sigma: cfg.Networking.Sigma,
		Mean:  cfg.Networking.Mean,
	}
	proposingLatency := &test.NormalLatencyModel{
		Sigma: cfg.Node.Legacy.ProposeIntervalSigma,
		Mean:  cfg.Node.Legacy.ProposeIntervalMean,
	}
	// Setup key pairs.
	prvKeys, pubKeys, err := test.NewKeys(int(cfg.Node.Num))
	if err != nil {
		log.Fatal("could not setup key pairs: ", err)
	}
	// Setup governance instance.
	gov, err := test.NewGovernance(
		test.NewState(
			pubKeys,
			time.Duration(cfg.Networking.Mean)*time.Millisecond,
			&common.NullLogger{},
			true,
		), core.ConfigRoundShift)
	if err != nil {
		log.Fatal("could not setup governance: ", err)
	}
	// Setup nodes and other consensus related stuffs.
	nodes, err := integration.PrepareNodes(
		gov, prvKeys, uint32(cfg.Node.Num), networkLatency, proposingLatency)
	if err != nil {
		log.Fatal("could not setup nodes: ", err)
	}
	apps, dbs := integration.CollectAppAndDBFromNodes(nodes)
	blockPerNode := int(math.Ceil(
		float64(cfg.Node.MaxBlock) / float64(cfg.Node.Num)))
	sch := test.NewScheduler(
		test.NewStopByConfirmedBlocks(blockPerNode, apps, dbs))
	now := time.Now().UTC()
	for _, v := range nodes {
		v.Bootstrap(sch, now)
	}
	// Run the simulation.
	sch.Run(cfg.Scheduler.WorkerNum)
	if err = integration.VerifyApps(apps); err != nil {
		log.Fatal("consensus result is not incorrect: ", err)
	}
	// Prepare statistics.
	stats, err := integration.NewStats(sch.CloneExecutionHistory(), apps)
	if err != nil {
		log.Fatal("could not generate statistics: ", err)
	}
	if *memprofile != "" {
		f, err := os.Create(*memprofile)
		if err != nil {
			log.Fatal("could not create memory profile: ", err)
		}
		runtime.GC() // get up-to-date statistics
		if err := pprof.WriteHeapProfile(f); err != nil {
			log.Fatal("could not write memory profile: ", err)
		}
		f.Close()
	}

	log.Printf("BPS: %v\n", stats.BPS)
	log.Printf("ExecutionTime: %v\n", stats.ExecutionTime)
	log.Printf("Prepare: %v\n", time.Duration(stats.All.PrepareExecLatency))
	log.Printf("Process: %v\n", time.Duration(stats.All.ProcessExecLatency))
}
