package benchmark

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"math/rand"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/urfave/cli"
)

type Request struct {
	Version string      `json:"jsonrpc"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params"`
	ID      int         `json:"id"`
}

type RPCError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

type Response struct {
	Error  *RPCError       `json:"error"`
	ID     int             `json:"id"`
	Result json.RawMessage `json:"result,omitempty"`
}

var (
	HOST = os.Getenv("HOST")

	SendTx = cli.Command{
		Name:      "sendtx",
		ShortName: "s",
		Usage:     "Command to send transactions",
		Action:    sendTx,
		Flags: []cli.Flag{
			cli.IntFlag{Name: "duration, d", Value: 10, Hidden: false, Usage: "test duration in seconds"},
			cli.IntFlag{Name: "txcount, c", Value: 100, Hidden: false, Usage: "test transaction count"},
		},
	}
	Analyze = cli.Command{
		Name:      "analyze",
		ShortName: "a",
		Usage:     "Analyze the receipts.json file. Output will be the blocks and corresponding transactions included in those blocks.",
		Action:    analyze,
		Flags:     []cli.Flag{},
	}
)

func getRandAcct(min, max int) int {
	rand.Seed(time.Now().UnixNano())
	return rand.Intn(max-min+1) + min
}

func createRequest(method string, params interface{}) Request {
	return Request{
		Version: "2.0",
		Method:  method,
		Params:  params,
		ID:      1,
	}
}

func call(method string, params interface{}) (*Response, error) {
	if HOST == "" {
		HOST = "http://localhost:8545"
	}

	req, err := json.Marshal(createRequest(method, params))
	if err != nil {
		return nil, err
	}

	var rpcRes *Response
	time.Sleep(1000000 * time.Nanosecond)
	/* #nosec */
	res, err := http.Post(HOST, "application/json", bytes.NewBuffer(req))
	if err != nil {
		return nil, err
	}

	decoder := json.NewDecoder(res.Body)
	rpcRes = new(Response)
	err = decoder.Decode(&rpcRes)
	if err != nil {
		return nil, err
	}

	err = res.Body.Close()
	if err != nil {
		return nil, err
	}

	return rpcRes, nil
}

func getTransactionReceipt(hash hexutil.Bytes) (map[string]interface{}, error) {
	param := []string{hash.String()}
	rpcRes, err := call("eth_getTransactionReceipt", param)
	if err != nil {
		return nil, err
	}

	receipt := make(map[string]interface{})
	err = json.Unmarshal(rpcRes.Result, &receipt)
	if err != nil {
		return nil, err
	}

	return receipt, nil
}

func waitForReceipt(hash hexutil.Bytes) (map[string]interface{}, error) {
	for i := 0; i < 10; i++ {
		receipt, err := getTransactionReceipt(hash)
		if receipt != nil {
			return receipt, err
		}

		time.Sleep(time.Second)
	}
	return nil, nil
}

func getAllReceipts(hashes []hexutil.Bytes) []map[string]interface{} {
	var receipts []map[string]interface{}
	for _, hash := range hashes {
		receipt, err := waitForReceipt(hash)
		if err != nil {
			fmt.Println(err)
			return nil
		}
		receipts = append(receipts, receipt)
	}
	return receipts
}

func checkRepeats(list []string, item string) []string {
	exist := false
	for _, litem := range list {
		if litem == item {
			exist = true
		}
	}
	if !exist {
		list = append(list, item)
	}
	return list
}

func sendTx(ctx *cli.Context) error {
	rpcRes, err := call("eth_accounts", []string{})
	if err != nil {
		return err
	}

	var accts []string
	err = json.Unmarshal(rpcRes.Result, &accts)
	if err != nil {
		return err
	}

	if len(accts) == 0 {
		fmt.Println("no accounts available")
		return nil
	}

	//remove facuet from list of accts
	accts = accts[1:]

	var nonces = make([]int, len(accts))

	value := "0x3B9ACA00"   //
	gasLimit := "0x5208"    //
	gasPrice := "0x15EF3C0" //
	txs := 0

	txTicker := time.NewTicker(time.Duration(500000) * time.Nanosecond)
	defer txTicker.Stop()
	testDuration := time.NewTicker(time.Duration(10000) * time.Second)
	defer testDuration.Stop()

	echan := make(chan error)

	hashes := []hexutil.Bytes{}
	nonceIncIndex := 0
	var wg sync.WaitGroup

	for {
		select {
		case <-txTicker.C:
			txs++
			if txs >= ctx.Int("txcount") {
				wg.Wait()
				txTicker.Stop()
				testDuration.Stop()
				receipts := getAllReceipts(hashes)

				hashesf, err := os.Create("/ethermint/docker/benchmarking/hashes.json")
				if err != nil {
					return err
				}
				hashesJson, err := json.Marshal(hashes)
				if err != nil {
					return err
				}
				hashesf.Write(hashesJson)

				receiptsf, err := os.Create("/ethermint/docker/benchmarking/receipts.json")
				if err != nil {
					return err
				}
				receiptsJson, err := json.Marshal(receipts)
				if err != nil {
					return err
				}
				receiptsf.Write(receiptsJson)

				return nil
			}

			wg.Add(1)

			fromIndex := getRandAcct(0, len(accts)-1)
			nonceIncIndex = fromIndex
			from := accts[fromIndex]
			toIndex := getRandAcct(0, len(accts)-1)
			to := accts[toIndex]

			if string(from) == string(to) {
				to = accts[getRandAcct(0, len(accts)-1)]
			}

			param := make([]map[string]string, 1)
			param[0] = make(map[string]string)

			param[0]["from"] = fmt.Sprintf("%s", from)
			param[0]["to"] = fmt.Sprintf("%s", to)
			param[0]["value"] = value
			param[0]["gasLimit"] = gasLimit
			param[0]["gasPrice"] = gasPrice
			param[0]["nonce"] = "0x" + fmt.Sprintf("%x", nonces[fromIndex])

			rpcTxRes, err := call("eth_sendTransaction", param)
			if err != nil {
				fmt.Println(err)
				echan <- err
			}

			var hash hexutil.Bytes
			err = json.Unmarshal(rpcTxRes.Result, &hash)
			if err != nil {
				fmt.Println(err)
				echan <- err
			}
			hashes = append(hashes, hash)

			nonces[nonceIncIndex]++

			wg.Done()

		case <-testDuration.C:
			txTicker.Stop()
			testDuration.Stop()
			return nil
		case err := <-echan:
			fmt.Printf("received err on channel:\n%v", err)
			return err
		}
	}
}

func analyze(ctx *cli.Context) error {
	//parse block numbers
	//call eth_getBlockTransactionCountByNumber to get tx counts

	receiptsf, err := ioutil.ReadFile("/ethermint/docker/benchmarking/receipts.json")
	if err != nil {
		fmt.Println("Unable to locate receipts.json file. Please run the sendtx command to generate this file.")
		return err
	}
	var receipts []map[string]interface{}

	err = json.Unmarshal(receiptsf, &receipts)
	if err != nil {
		return err
	}

	blocks := []string{}
	for _, receipt := range receipts {
		fmt.Println(receipt["blockNumber"])
		blockn := fmt.Sprintf("%s", receipt["blockNumber"])
		blocks = checkRepeats(blocks, blockn)
	}
	fmt.Println(blocks)

	return nil
}
