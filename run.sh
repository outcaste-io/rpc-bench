go build . || exit

echo "URL is $URL"
if [ -z "$URL" ]; then echo "Please export URL"; exit; fi

dur=60s

./rpc-bench --rpc=$URL --method=eth_getBlockByHash --dur=$dur
./rpc-bench --rpc=$URL --method=eth_getBlockByNumber --dur=$dur
./rpc-bench --rpc=$URL --method=eth_getBlockTransactionCountByHash --dur=$dur
./rpc-bench --rpc=$URL --method=eth_getBlockTransactionCountByNumber --dur=$dur
./rpc-bench --rpc=$URL --method=eth_getTransactionByHash --dur=$dur
./rpc-bench --rpc=$URL --method=eth_getTransactionReceipt --dur=$dur
