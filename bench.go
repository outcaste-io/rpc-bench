// Copyright 2022 Outcaste LLC. Licensed under the Apache License v2.0.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"math/rand"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/outcaste-io/lib/x"
	"github.com/outcaste-io/lib/y"
	"github.com/outcaste-io/ristretto/z"
	"github.com/pkg/errors"
)

var (
	rpc    = flag.String("rpc", "", "JSON-RPC endpoint")
	gor    = flag.Int("j", 4, "Num Goroutines to use")
	dur    = flag.Duration("dur", time.Minute, "How long to run the benchmark")
	all    = flag.Bool("all", false, "Retrieve all fields.")
	sample = flag.Int("sample", 10000, "Output query and response every N times")
)

type Log struct {
	BlockNumber string `json:"blockNumber"`
}

type Txn struct {
	Hash        string
	BlockNumber string `json:"blockNumber"`
	Logs        []Log  `json:"logs"`
}

type Block struct {
	Number       string
	Transactions []Txn
}

type Error struct {
	Code    int
	Message string
}
type BlockResp struct {
	Result Block
	Error  Error
}
type TxnResp struct {
	Result Txn
	Error  Error
}

func callRPC(client *http.Client, q string) ([]byte, error) {
	atomic.AddUint64(&numQueries, 1)

	for i := 0; ; i++ {
		buf := bytes.NewBufferString(q)
		req, err := http.NewRequest("POST", *rpc, buf)
		x.Check(err)
		req.Header.Add("Content-Type", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			return nil, errors.Wrapf(err, "client.Do")
		}
		if resp.StatusCode == 200 {
			// OK
		} else if resp.StatusCode == 429 {
			atomic.AddUint64(&numLimits, 1)
			time.Sleep(time.Second)
			continue
		} else {
			fmt.Printf("got status code: %d\n", resp.StatusCode)
			os.Exit(1)
		}
		data, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			return nil, errors.Wrapf(err, "readall")
		}
		resp.Body.Close()
		if len(data) == 0 {
			fmt.Printf("len(data) == 0\n")
			os.Exit(1)
		}

		ds := string(data)
		if strings.Contains(ds, `"error":`) {
			if strings.Contains(ds, `"error":{"code":429,`) {
				atomic.AddUint64(&numLimits, 1)
				// fmt.Println("Rate limited. Sleeping for a sec")
				time.Sleep(time.Second)
				continue
			}
			fmt.Printf("Got error response: %s\n", ds)
			os.Exit(1)
		}
		return data, nil
	}
}

// The CUs are derived from:
// https://docs.alchemy.com/reference/compute-units
// Quick CUs are derived from:
// https://www.quicknode.com/api-credits/eth
func fetchBlockWithTxnAndLogsWithRPC(client *http.Client, blockNum int64) (int64, error) {
	if client == nil {
		client = &http.Client{}
	}
	hno := hexutil.EncodeUint64(uint64(blockNum))
	q := fmt.Sprintf(`{"jsonrpc":"2.0","method":"eth_getBlockByNumber","params":[%q, true],"id":1}`, hno)
	// fmt.Printf("Block Query: %s\n", q)
	data, err := callRPC(client, q)
	x.Check(err)
	atomic.AddUint64(&numCUs, 16)
	atomic.AddUint64(&numQuickCUs, 2)
	sz := int64(len(data))

	var resp BlockResp
	if err := json.Unmarshal(data, &resp); err != nil {
		fmt.Printf("Got invalid block resp data: %s\n", data)
		os.Exit(1)
	}
	if resp.Result.Number != hno {
		fmt.Printf("Got result: %+v. Expecting: %s Test Failed.\n", resp.Result, hno)
		fmt.Printf("Response: %s\n", data)
		os.Exit(1)
	}
	for _, txn := range resp.Result.Transactions {
		if txn.BlockNumber != hno {
			fmt.Printf("Got result: %+v. Expecting: %s Test Failed.\n", resp.Result)
			fmt.Printf("Response: %s\n", data)
			os.Exit(1)
		}
		q = fmt.Sprintf(
			`{"jsonrpc":"2.0","method":"eth_getTransactionReceipt","params":[%q],"id":1}`, txn.Hash)
		// fmt.Printf("Receipt query: %s\n", q)
		data, err = callRPC(client, q)
		x.Check(err)
		atomic.AddUint64(&numCUs, 15)
		atomic.AddUint64(&numQuickCUs, 2)
		sz += int64(len(data))

		var txnResp TxnResp
		if err := json.Unmarshal(data, &txnResp); err != nil {
			fmt.Printf("Got invalid txn resp data: %q\n | query was: %s | error: %v", data, q, err)
			os.Exit(1)
		}
		if txnResp.Result.BlockNumber != hno {
			fmt.Printf("Got result: %+v. Expecting: %s Test Failed.\n", txnResp.Result)
			fmt.Printf("Response: %s\n", data)
			os.Exit(1)
		}
		for _, log := range txnResp.Result.Logs {
			if log.BlockNumber != hno {
				fmt.Printf("Got result: %+v. Expecting: %s Test Failed.\n", txnResp.Result, hno)
				fmt.Printf("Response: %s\n", data)
				os.Exit(1)
			}
		}
	}
	return sz, nil
}

var numBlocks, numQueries, numBytes, numLimits, numCUs, numQuickCUs uint64

func printQps() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	start := time.Now()
	rm := y.NewRateMonitor(300)
	for range ticker.C {
		numB := atomic.LoadUint64(&numBlocks)
		rm.Capture(numB)
		numQ := atomic.LoadUint64(&numQueries)
		numL := atomic.LoadUint64(&numLimits)
		numC := atomic.LoadUint64(&numCUs)
		quiC := atomic.LoadUint64(&numQuickCUs)
		bytes := atomic.LoadUint64(&numBytes)

		dur := time.Since(start)
		fmt.Printf("Num Blocks: %5d | Num Queries: %4d | Num 429: %4d | Alchemy CUs: %4d | QuickNode CUs: %4d | Data: %s [ %6s @ %d blocks/sec ]\n",
			numB, numQ, numL, numC, quiC,
			humanize.IBytes(bytes), dur.Round(time.Second), rm.Rate())
	}
}

func main() {
	flag.Parse()

	rand.Seed(time.Now().UnixNano())
	end := time.Now().Add(*dur)
	fmt.Printf("Time now: %s . Ending at %s\n",
		time.Now().Truncate(time.Second), end.Truncate(time.Second))

	go printQps()

	var mu sync.Mutex
	bounds := z.HistogramBounds(0, 10)
	last := float64(2048)
	for i := 0; i < 1024; i++ {
		bounds = append(bounds, last)
		last += 1024.0
	}
	fmt.Printf("Bounds are: %+v\n", bounds)
	histDur := z.NewHistogramData(bounds)
	histSz := z.NewHistogramData(bounds)

	N := *maxBlock - *minBlock
	var wg sync.WaitGroup
	for i := 0; i < *gor; i++ {
		wg.Add(1)

		go func() {
			defer wg.Done()

			client := &http.Client{}
			var times []int64
			var sizes []int64
			for i := int64(0); ; i++ {
				ts := time.Now()
				if ts.After(end) {
					break
				}
				bno := rand.Int63n(N) + *minBlock

				var sz int64
				var err error
				if len(*rpc) > 0 {
					sz, err = fetchBlockWithTxnAndLogsWithRPC(client, bno)
				} else {
					log.Fatalf("JSON-RPC URL should be provided")
				}
				x.Check(err)

				times = append(times, time.Since(ts).Milliseconds())
				sizes = append(sizes, sz)
				atomic.AddUint64(&numBytes, uint64(sz))
				atomic.AddUint64(&numBlocks, 1)
			}
			mu.Lock()
			for _, t := range times {
				histDur.Update(t)
			}
			for _, sz := range sizes {
				histSz.Update(sz)
			}
			mu.Unlock()
		}()
	}
	wg.Wait()

	fmt.Println("-----------------------")
	fmt.Printf("Latency in milliseconds")
	fmt.Println(histDur.String())

	fmt.Println("-----------------------")
	fmt.Printf("Resp size in bytes")
	fmt.Println(histSz.String())

	time.Sleep(2 * time.Second)
	fmt.Println("DONE")
}
