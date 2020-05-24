/*

Contains types to represent nodes contained in Docker containers.

*/

package ava_commons

import (
	"github.com/gmarchetti/kurtosis/commons/testnet"
)

// Type representing a Gecko Node and which ports on the host machine it will use for HTTP and Staking.
type GeckoServiceConfig struct {
	// Will be empty if this node should be a boot node
	bootNodes      map[testnet.JsonRpcServiceSocket]testnet.JsonRpcRequest
	geckoImageName string
}

const (
	STAKING_PORT_ID testnet.ServiceSpecificPort = 0

	HTTP_PORT = 9650
	STAKING_PORT = 9651
)

// TODO implement more Ava-specific params here, like snow quorum
// bootNodes should contain a mapping of bootnode socket -> liveness request to make to verify it's up
func NewGeckoServiceConfig(dockerImage string, bootNodes map[testnet.JsonRpcServiceSocket]testnet.JsonRpcRequest) *GeckoServiceConfig {
	return &GeckoServiceConfig{
		bootNodes:      bootNodes,
		geckoImageName: dockerImage,
	}
}

func (g GeckoServiceConfig) GetDockerImage() string {
	return g.geckoImageName
}

func (g GeckoServiceConfig) GetJsonRpcPort() int {
	return HTTP_PORT
}

func (g GeckoServiceConfig) GetOtherPorts() map[testnet.ServiceSpecificPort]int {
	result := make(map[testnet.ServiceSpecificPort]int)
	result[STAKING_PORT_ID] = STAKING_PORT
	return result
}

// TODO actually return a different command based on the dependencies!
func (g GeckoServiceConfig) GetContainerStartCommand() []string {
	return []string{
		"/gecko/build/ava",
		"--public-ip=127.0.0.1",
		"--snow-sample-size=1",
		"--snow-quorum-size=1",
		"--staking-tls-enabled=false",
	}
}

func (g GeckoServiceConfig) GetLivenessRequest() *testnet.JsonRpcRequest {
	return &testnet.JsonRpcRequest{
		Endpoint:   "/ext/P",
		Method:     "platform.getCurrentValidators",
		RpcVersion: testnet.RPC_VERSION_1_0,
		Params:     make(map[string]string),
		ID:         1,   // Not really sure if we'd ever need to change this
	}
}

