go build . || exit

dur=60s

./rpc-bench --rpc $URL --method eth_getBlockByHash --dur=$dur
./rpc-bench --rpc $URL --method eth_getBlockByNumber --dur=$dur
./rpc-bench --rpc $URL --method eth_getBlockTransactionCountByHash --dur=$dur
./rpc-bench --rpc $URL --method eth_getBlockTransactionCountByNumber --dur=$dur
./rpc-bench --rpc $URL --method eth_getTransactionByHash --dur=$dur
./rpc-bench --rpc $URL --method eth_getTransactionReceipt --dur=$dur
