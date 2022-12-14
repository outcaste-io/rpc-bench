set -e

go build .

echo "URL is $URL"
if [ -z "$URL" ]; then echo "Please export URL"; exit; fi

dur=60s
sample=100
j=8

./rpc-bench -j=$j --rpc=$URL --secret=$SECRET --method=eth_getBlockByNumber --dur=$dur --sample=$sample
./rpc-bench -j=$j --rpc=$URL --secret=$SECRET --method=eth_getBlockByHash --dur=$dur --sample=$sample
./rpc-bench -j=$j --rpc=$URL --secret=$SECRET --method=eth_getTransactionByHash --dur=$dur --sample=$sample
./rpc-bench -j=$j --rpc=$URL --secret=$SECRET --method=eth_getTransactionReceipt --dur=$dur --sample=$sample
./rpc-bench -j=$j --rpc=$URL --secret=$SECRET --method=eth_getBlockTransactionCountByHash --dur=$dur --sample=$sample
./rpc-bench -j=$j --rpc=$URL --secret=$SECRET --method=eth_getBlockTransactionCountByNumber --dur=$dur --sample=$sample
