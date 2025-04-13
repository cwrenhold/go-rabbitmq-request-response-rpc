package handlers

import (
	"net/http"

	"client/rpc"
)

func HelloSQLHandler(w http.ResponseWriter, r *http.Request, rpcClient *rpc.RPCClient) {
	handleRPCRequest(w, r, rpcClient, "hello_sql",
		func() (interface{}, error) {
			return buildRequestMetadata(), nil
		},
		func(response []byte) ([]byte, error) {
			return response, nil
		})
}
