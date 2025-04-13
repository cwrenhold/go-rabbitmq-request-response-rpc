package handlers

import (
	"fmt"
	"net/http"
	"strconv"

	"client/requests"
	"client/rpc"
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

			return requests.AddRequest{
				RequestMetadata: buildRequestMetadata(),
				Values:          values,
			}, nil
		},
		func(response []byte) ([]byte, error) {
			return response, nil
		})
}
