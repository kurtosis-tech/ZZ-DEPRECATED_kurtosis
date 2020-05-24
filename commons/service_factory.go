package commons

import "github.com/palantir/stacktrace"

// This implicitly is a Docker container factory, but we could abstract to other backends if we wanted later
type ServiceFactory struct {
	config ServiceFactoryConfig
}

func NewServiceFactory(config ServiceFactoryConfig) *ServiceFactory {
	return &ServiceFactory{
		config: config,
	}
}

// TODO needing to pass in hte ipAddrOffset is a nasty awful hack here that will go away when the --public-ips flag is gone!
// If Go had generics, this would be genericized so that the arg type = return type
func (factory ServiceFactory) Construct(
			ipAddrOffset int,
			manager *DockerManager,
			dependencies []Service) (Service, string, error) {
	dockerImage := factory.config.GetDockerImage()
	startCmdArgs := factory.config.GetStartCommand(ipAddrOffset, dependencies)
	usedPorts := factory.config.GetUsedPorts()

	ipAddr, containerId, err := manager.CreateAndStartContainerForService(dockerImage, usedPorts, startCmdArgs)
	if err != nil {
		return nil, "", stacktrace.Propagate(err, "Could not start docker service for image %v", dockerImage)
	}
	return factory.config.GetServiceFromIp(ipAddr), containerId, nil
}
