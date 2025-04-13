package handlers

import (
	"client/requests"
	"client/rpc"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

func AddHandler(w http.ResponseWriter, r *http.Request, rpcClient *rpc.RPCClient) {
	handleRPCRequest(w, r, rpcClient, "add",
		func() (interface{}, error) {
			queryValues := r.URL.Query()["val"]
			if len(queryValues) == 0 {
				return nil, fmt.Errorf("missing 'val' query parameter")
			}

			values := make([]int, 0, len(queryValues))
			for _, valStr := range queryValues {
				val, err := strconv.Atoi(valStr)
				if err != nil {
					return nil, fmt.Errorf("invalid value '%s': must be an integer", valStr)
				}
				values = append(values, val)
			}

			currentTime := time.Now().Format(time.RFC3339)
			return requests.AddRequest{
				RequestMetadata: requests.RequestMetadata{
					SentAt: currentTime,
				},
				Values: values,
			}, nil
		},
		func(response []byte) ([]byte, error) {
			return response, nil
		})
}
