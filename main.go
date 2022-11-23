// Copyright 2022 Outcaste LLC. Licensed under the Apache License v2.0.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"math/rand"
	"net/http"
	"os"
	"strconv"
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
	"github.com/tidwall/gjson"
)

var (
	rpc    = flag.String("rpc", "", "JSON-RPC endpoint")
	secret = flag.String("secret", "", "Add a secret header")
	gor    = flag.Int("j", 4, "Num Goroutines to use")
	dur    = flag.Duration("dur", time.Minute, "How long to run the benchmark")
	method = flag.String("method", "", "Which ETH method to benchmark")
	sample = flag.Int("sample", 1000, "Dump output into file every N queries")
)

type Stats struct {
	sync.Mutex
	histDur *z.HistogramData
	histSz  *z.HistogramData
}

func InitStats() *Stats {
	bounds := z.HistogramBounds(0, 4)
	last := float64(16)
	for i := 0; i < 2048; i++ {
		bounds = append(bounds, last)
		last += 16.0
	}
	// fmt.Printf("Bounds are: %+v\n", bounds)
	st := &Stats{}
	st.histDur = z.NewHistogramData(bounds)
	st.histSz = z.NewHistogramData(z.HistogramBounds(0, 20))
	return st
}

func (st *Stats) Update(dur, sz int64) {
	st.Lock()
	st.histDur.Update(dur)
	st.histSz.Update(sz)
	st.Unlock()
}

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
	numQ := atomic.AddUint64(&numQueries, 1)

	start := time.Now()
	sz := int64(0)
	defer func() {
		dur := time.Since(start).Milliseconds()
		stats.Update(dur, sz)
		atomic.AddUint64(&numBytes, uint64(sz))
		atomic.AddUint64(&numCalls, 1)
	}()

	for i := 0; ; i++ {
		buf := bytes.NewBufferString(q)
		req, err := http.NewRequest("POST", *rpc, buf)
		x.Check(err)
		req.Header.Add("Content-Type", "application/json")
		if len(*secret) > 0 {
			req.Header.Add("Secret-Header", *secret)
		}

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
		sz = int64(len(data))

		if numQ%(uint64(*sample)) == 0 {
			x.Check2(sampleBuf.Write(data))
			sampleBuf.WriteRune('\n')
		}

		if bytes.Contains(data, []byte(`"error":`)) {
			// if bytes.Contains(data, []byte(`"error":{"code":429,`)) {
			// 	atomic.AddUint64(&numLimits, 1)
			// 	// fmt.Println("Rate limited. Sleeping for a sec")
			// 	time.Sleep(time.Second)
			// 	continue
			// }
			fmt.Printf("Got error response: %s\n", data)
			os.Exit(1)
		}
		return data, nil
	}
}

// Returns time and size
func fetchBlockByNumber(client *http.Client, blockNum int64) {
	if blockNum == 0 {
		panic("blockNum is zero")
	}
	hno := hexutil.EncodeUint64(uint64(blockNum))
	q := fmt.Sprintf(`{"jsonrpc":"2.0","method":"eth_getBlockByNumber","params":[%q, true],"id":1}`, hno)
	// fmt.Printf("Block Query: %s\n", q)

	data, err := callRPC(client, q)
	x.Check(err)
	atomic.AddUint64(&numCUs, 16)
	atomic.AddUint64(&numQuickCUs, 2)

	numRes := gjson.GetBytes(data, "result.number")
	if numRes.Str != hno {
		fmt.Printf("Got result: %+v. Expecting: %s Test Failed.\n", numRes.Str, hno)
		fmt.Printf("Response: %s\n", data)
		os.Exit(1)
	}
}
func fetchBlockByHash(client *http.Client, hash string) {
	if len(hash) == 0 {
		panic("hash is empty")
	}
	q := fmt.Sprintf(`{"jsonrpc":"2.0","method":"eth_getBlockByHash","params":[%q, true],"id":1}`, hash)
	// fmt.Printf("Block Query: %s\n", q)

	data, err := callRPC(client, q)
	x.Check(err)
	atomic.AddUint64(&numCUs, 16)
	atomic.AddUint64(&numQuickCUs, 2)

	hashRes := gjson.GetBytes(data, "result.hash")
	if hashRes.Str != hash {
		fmt.Printf("Got result: %+v. Expecting: %s Test Failed.\n", hashRes.Str, hash)
		fmt.Printf("Response: %s\n", data)
		os.Exit(1)
	}
}
func fetchTxnByHash(client *http.Client, hash string) {
	if len(hash) == 0 {
		panic("hash is empty")
	}
	q := fmt.Sprintf(`{"jsonrpc":"2.0","method":"eth_getTransactionByHash","params":[%q],"id":1}`, hash)
	// fmt.Printf("Block Query: %s\n", q)
	data, err := callRPC(client, q)
	x.Check(err)
	atomic.AddUint64(&numCUs, 16)
	atomic.AddUint64(&numQuickCUs, 2)

	hashRes := gjson.GetBytes(data, "result.hash")
	if hashRes.Str != hash {
		fmt.Printf("Got result: %+v. Expecting: %s Test Failed.\n", hashRes.Str, hash)
		fmt.Printf("Response: %s\n", data)
		os.Exit(1)
	}
}
func fetchTxnReceipt(client *http.Client, hash string) {
	if len(hash) == 0 {
		panic("hash is empty")
	}
	q := fmt.Sprintf(`{"jsonrpc":"2.0","method":"eth_getTransactionReceipt","params":[%q],"id":1}`, hash)
	// fmt.Printf("Block Query: %s\n", q)
	data, err := callRPC(client, q)
	x.Check(err)
	atomic.AddUint64(&numCUs, 16)
	atomic.AddUint64(&numQuickCUs, 2)

	hashRes := gjson.GetBytes(data, "result.transactionHash")
	if hashRes.Str != hash {
		fmt.Printf("Got result: %+v. Expecting: %s Test Failed.\n", hashRes.Str, hash)
		fmt.Printf("Response: %s\n", data)
		os.Exit(1)
	}
}
func fetchTxnCountByHash(client *http.Client, hash string) {
	if len(hash) == 0 {
		panic("hash is empty")
	}
	q := fmt.Sprintf(`{"jsonrpc":"2.0","method":"eth_getBlockTransactionCountByHash","params":[%q],"id":1}`, hash)
	// fmt.Printf("Block Query: %s\n", q)
	_, err := callRPC(client, q)
	x.Check(err)
	atomic.AddUint64(&numCUs, 16)
	atomic.AddUint64(&numQuickCUs, 2)
}
func fetchTxnCountByNumber(client *http.Client, bnum int64) {
	if bnum == 0 {
		panic("bnum is 0")
	}
	hno := hexutil.EncodeUint64(uint64(bnum))
	q := fmt.Sprintf(`{"jsonrpc":"2.0","method":"eth_getBlockTransactionCountByNumber","params":[%q],"id":1}`, hno)
	// fmt.Printf("Block Query: %s\n", q)
	_, err := callRPC(client, q)
	x.Check(err)
	atomic.AddUint64(&numCUs, 16)
	atomic.AddUint64(&numQuickCUs, 2)
}

// The CUs are derived from:
// https://docs.alchemy.com/reference/compute-units
// Quick CUs are derived from:
// https://www.quicknode.com/api-credits/eth
func fetchBlockWithTxnAndLogsWithRPC(client *http.Client, blockNum int64) (int64, error) {
	x.AssertTrue(client != nil)
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

var numCalls, numQueries, numBytes, numLimits, numCUs, numQuickCUs uint64

func printQps() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	start := time.Now()
	rm := y.NewRateMonitor(300)
	for range ticker.C {
		numB := atomic.LoadUint64(&numCalls)
		rm.Capture(numB)
		numQ := atomic.LoadUint64(&numQueries)
		numL := atomic.LoadUint64(&numLimits)
		// numC := atomic.LoadUint64(&numCUs)
		// quiC := atomic.LoadUint64(&numQuickCUs)
		bytes := atomic.LoadUint64(&numBytes)

		dur := time.Since(start)
		fmt.Printf("Num Blocks: %5d | Num Queries: %4d | Num 429: %4d | Data: %s [ %6s @ %d calls/sec ]\n",
			numB, numQ, numL,
			humanize.IBytes(bytes), dur.Round(time.Second), rm.Rate())
	}
}

type Input struct {
	blockHashes  []string
	blockNumbers []int64
	txnHashes    []string
}

func LoadInput() Input {
	var input Input
	data, err := ioutil.ReadFile("eth-blockhashes.txt")
	x.Check(err)
	input.blockHashes = strings.Split(string(data), "\n")

	out := input.blockHashes[:0]
	for _, hash := range input.blockHashes {
		if len(hash) == 0 {
			continue
		}
		out = append(out, hash)
	}
	input.blockHashes = out

	data, err = ioutil.ReadFile("eth-blocks.txt")
	x.Check(err)
	bnos := strings.Split(string(data), "\n")
	for _, bno := range bnos {
		if len(bno) == 0 {
			continue
		}
		b64, err := strconv.Atoi(bno)
		x.Check(err)
		input.blockNumbers = append(input.blockNumbers, int64(b64))
	}

	data, err = ioutil.ReadFile("eth-txnhashes.txt")
	x.Check(err)
	input.txnHashes = strings.Split(string(data), "\n")
	out = input.txnHashes[:0]
	for _, hash := range input.txnHashes {
		if len(hash) == 0 {
			continue
		}
		out = append(out, hash)
	}
	input.txnHashes = out
	return input
}

var sampleBuf bytes.Buffer
var stats *Stats

func warmUp(client *http.Client) {
	for i := 0; i < 10; i++ {
		start := time.Now()
		buf := bytes.NewBufferString(`{"id": 1, "jsonrpc": "2.0", "method": "eth_chainId" }`)
		req, err := http.NewRequest("POST", *rpc, buf)
		x.Check(err)
		if len(*secret) > 0 {
			req.Header.Add("Secret-Header", *secret)
		}
		resp, err := client.Do(req)
		x.Check(err)
		data, err := ioutil.ReadAll(resp.Body)
		x.Check(err)
		fmt.Printf("Connection established with response [%d, %s]: %s\n",
			i, time.Since(start).Truncate(time.Millisecond), data)
		x.Check(resp.Body.Close())
	}
}

func main() {
	flag.Parse()

	stats = InitStats()
	input := LoadInput()
	rand.Seed(time.Now().UnixNano())

	sampleBuf.WriteString(fmt.Sprintf(`{"url": %q, "method": %q}`, *rpc, *method))
	sampleBuf.WriteRune('\n')

	defer func() {
		if len(sampleBuf.Bytes()) == 0 {
			return
		}
		f, err := ioutil.TempFile(".", "sample-"+*method+"-")
		x.Check(err)
		fmt.Printf("Writing samples to file: %s\n", f.Name())

		x.Check2(f.Write(sampleBuf.Bytes()))
		x.Check(f.Sync())
		x.Check(f.Close())
	}()

	clients := make([]*http.Client, *gor)
	for i := 0; i < *gor; i++ {
		clients[i] = &http.Client{
			Transport: &http.Transport{},
		}
		warmUp(clients[i])
		fmt.Printf("Warmed up client: %d\n", i)
	}

	fmt.Printf("Method: %s | START\n", *method)
	end := time.Now().Add(*dur)
	fmt.Printf("Time now: %s . Ending at %s\n",
		time.Now().Truncate(time.Second), end.Truncate(time.Second))

	go printQps()

	cumIdx := rand.Int63n(1000000)
	fmt.Printf("start Idx: %d\n", cumIdx)

	var wg sync.WaitGroup
	for i := 0; i < *gor; i++ {
		wg.Add(1)

		go func(idx int) {
			defer wg.Done()

			client := clients[idx]
			x.AssertTrue(client != nil)

			for i := int64(0); ; i++ {
				ts := time.Now()
				if ts.After(end) {
					break
				}

				idx := atomic.AddInt64(&cumIdx, 1)
				switch *method {
				case "eth_getBlockByHash":
					idx = idx % int64(len(input.blockHashes))
					fetchBlockByHash(client, input.blockHashes[idx])
				case "eth_getBlockByNumber":
					idx = idx % int64(len(input.blockNumbers))
					fetchBlockByNumber(client, input.blockNumbers[idx])
				case "eth_getBlockTransactionCountByHash":
					idx = idx % int64(len(input.blockHashes))
					fetchTxnCountByHash(client, input.blockHashes[idx])
				case "eth_getBlockTransactionCountByNumber":
					idx = idx % int64(len(input.blockNumbers))
					fetchTxnCountByNumber(client, input.blockNumbers[idx])
				case "eth_getTransactionByHash":
					idx = idx % int64(len(input.txnHashes))
					fetchTxnByHash(client, input.txnHashes[idx])
				case "eth_getTransactionReceipt":
					idx = idx % int64(len(input.txnHashes))
					fetchTxnReceipt(client, input.txnHashes[idx])
				default:
					fmt.Printf("Invalid method")
					os.Exit(1)
				}
			}
		}(i)
	}
	wg.Wait()

	fmt.Println("-----------------------")
	fmt.Printf("Latency in milliseconds")
	fmt.Println(stats.histDur.String())

	fmt.Println("-----------------------")
	fmt.Printf("Resp size in bytes")
	fmt.Println(stats.histSz.String())

	time.Sleep(2 * time.Second)
	fmt.Printf("Method: %s | DONE\n", *method)
}
