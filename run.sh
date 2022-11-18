go build . || exit

echo "URL is $URL"
if [ -z "$URL" ]; then echo "Please export URL"; exit; fi

dur=60s
sample=100

./rpc-bench --rpc=$URL --method=eth_getBlockByHash --dur=$dur --sample=$sample
./rpc-bench --rpc=$URL --method=eth_getBlockByNumber --dur=$dur --sample=$sample
./rpc-bench --rpc=$URL --method=eth_getTransactionByHash --dur=$dur --sample=$sample
./rpc-bench --rpc=$URL --method=eth_getTransactionReceipt --dur=$dur --sample=$sample
./rpc-bench --rpc=$URL --method=eth_getBlockTransactionCountByHash --dur=$dur --sample=$sample
./rpc-bench --rpc=$URL --method=eth_getBlockTransactionCountByNumber --dur=$dur --sample=$sample
