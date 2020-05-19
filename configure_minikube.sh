## From https://medium.com/bb-tutorials-and-thoughts/how-to-use-own-local-doker-images-with-minikube-2c1ed0b0968

# Sets docker environemt to be Minikube's docker environment.
eval $(minikube docker-env)
# Build gecko image and register it with Minikube's docker environment.
$GOPATH/src/github.com/ava-labs/gecko/scripts/build_image.sh
