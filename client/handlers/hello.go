package handlers

import (
	"client/requests"
	"client/rpc"
	"net/http"
	"time"
)

func HelloHandler(w http.ResponseWriter, r *http.Request, rpcClient *rpc.RPCClient) {
	handleRPCRequest(w, r, rpcClient, "hello",
		func() (interface{}, error) {
			currentTime := time.Now().Format(time.RFC3339)
			return requests.RequestMetadata{SentAt: currentTime}, nil
		},
		func(response []byte) ([]byte, error) {
			return response, nil
		})
}
