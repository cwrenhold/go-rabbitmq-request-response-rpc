package handlers

import (
	"net/http"

	"client/rpc"
)

func HelloHandler(w http.ResponseWriter, r *http.Request, rpcClient *rpc.RPCClient) {
	handleRPCRequest(w, r, rpcClient, "hello",
		func() (interface{}, error) {
			return buildRequestMetadata(), nil
		},
		func(response []byte) ([]byte, error) {
			return response, nil
		})
}
