# kurtosis
E2E Testing Harness for Ava

# Requirements

Golang version 1.13.x
Docker Engine running in your environment.

# Install

Clone this repository and cd into it.  
Run `go install`. This will build the main binary and put it on your path.  

# Usage

Run `kurtosis -help` or `kurtosis -h` to see command line usage.

# Architecture

Kurtosis runs a prebuilt Gecko image, which must already exist in your Docker engine.  
The name of this image is specified by a command line argument.
Currently, the ports that the container will run on for HTTP and for staking on your host machine are hard-coded to the standard Gecko defaults - 9650 for HTTP, 9651 for staking.

# Helpful Tip

Create an alias in your shell .rc file to stop and clear all Docker containers in one line.
Run this every time after you kill kurtosis, because the containers will hang around.
One way to do this is as follow:

```
dockerclearall() { docker stop $(docker ps -a -q); docker rm -v $(docker ps -a -q) }
alias dclear=dockerclearall
```

# TODO

* Run multiple containers with different container-host port mappings for HTTP and staking.
* Run boot node (no peers) and then point subsequent containers to existing containers as they start up.
* Ability to run spectators versus stakers
* Create a testing container that triggers RPC calls against a target node. 

