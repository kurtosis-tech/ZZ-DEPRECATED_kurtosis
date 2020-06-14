package networks

import (
	"github.com/kurtosis-tech/kurtosis/commons/services"
	"gotest.tools/assert"
	"os"
	"testing"
	"time"
)

// ======================== Test Service ========================
type TestService struct {
	result string
}
func (service TestService) GetTestEndpointResult() string {
	return service.result
}


// ======================== Test Initializer Core ========================
type TestInitializerCore struct {
	lastDependencyResults map[string]bool
	serviceResult string
}

func NewTestInitializerCore(lastDependencyResults map[string]bool, serviceResult string) *TestInitializerCore {
	return &TestInitializerCore{lastDependencyResults: lastDependencyResults, serviceResult: serviceResult}
}



func (t TestInitializerCore) GetUsedPorts() map[int]bool {
	return make(map[int]bool)
}

func (t TestInitializerCore) GetStartCommand(publicIpAddr string, dependencies []services.Service) ([]string, error) {
	castedServices := make([]TestService, 0, len(dependencies))
	for _, service := range dependencies {
		castedServices = append(castedServices, service.(TestService))
	}

	dependencyResults := make(map[string]bool)
	for _, service := range castedServices {
		dependencyResults[service.GetTestEndpointResult()] = true
	}

	t.lastDependencyResults = dependencyResults
	return make([]string, 0, 0), nil
}

func (t TestInitializerCore) GetServiceFromIp(ipAddr string) services.Service {
	return TestService{}
}


func (t TestInitializerCore) GetFilepathsToMount() map[string]bool {
	return make(map[string]bool)
}

func (t TestInitializerCore) InitializeMountedFiles(filepathsToMount map[string]*os.File, dependencies []services.Service) error {
	return nil
}

// Returns a set of the results of calling "GetTestEndpointResult" on each dependency service
func (t TestInitializerCore) GetLastDependencyResults() map[string]bool {
	return t.lastDependencyResults
}

// The serviceResult is the string that a service constructed with this InitializerCore will return
func getTestInitializerCore(serviceResult string) *TestInitializerCore {
	return NewTestInitializerCore(make(map[string]bool), serviceResult)
}


// ======================== Test Availability Checker Core ========================
type TestAvailabilityCheckerCore struct {}
func (t TestAvailabilityCheckerCore) IsServiceUp(toCheck services.Service, dependencies []services.Service) bool {
	return true
}
func (t TestAvailabilityCheckerCore) GetTimeout() time.Duration {
	return 30 * time.Second
}
func getTestCheckerCore() services.ServiceAvailabilityCheckerCore {
	return TestAvailabilityCheckerCore{}
}

// ======================== Tests ========================
func TestDisallowingNonexistentConfigs(t *testing.T) {
	builder := NewServiceNetworkConfigBuilder()
	_, err := builder.AddService(0, 0, make(map[int]bool))
	if err == nil {
		t.Fatal("Expected error when declaring a service with a configuration that doesn't exist")
	}
}

func TestDisallowingNonexistentDependencies(t *testing.T) {
	builder := NewServiceNetworkConfigBuilder()
	config := builder.AddTestImageConfiguration(getTestInitializerCore("test"), getTestCheckerCore())

	dependencies := map[int]bool{
		0: true,
	}

	_, err := builder.AddService(config, 0, dependencies)
	if err == nil {
		t.Fatal("Expected error when declaring a dependency on a service ID that doesn't exist")
	}
}


func TestConfigurationIdsDifferent(t *testing.T) {
	testImage := "testImage"
	idSet := make(map[int]bool)
	builder := NewServiceNetworkConfigBuilder()
	config1 := builder.AddTestImageConfiguration(getTestInitializerCore("test1"), getTestCheckerCore())
	idSet[config1] = true
	config2 := builder.AddStaticImageConfiguration(&testImage, getTestInitializerCore("test2"), getTestCheckerCore())
	idSet[config2] = true
	config3 := builder.AddTestImageConfiguration(getTestInitializerCore("test3"), getTestCheckerCore())
	idSet[config3] = true
	config4 := builder.AddStaticImageConfiguration(&testImage, getTestInitializerCore("test4"), getTestCheckerCore())
	idSet[config4] = true
	config5 := builder.AddTestImageConfiguration(getTestInitializerCore("test5"), getTestCheckerCore())
	idSet[config5] = true
	assert.Assert(t, len(idSet) == 5, "IDs should be different.")
}

func TestIdsDifferent(t *testing.T) {
	builder := NewServiceNetworkConfigBuilder()
	config := builder.AddTestImageConfiguration(getTestInitializerCore("test"), getTestCheckerCore())
	svc1, err := builder.AddService(config, 1, make(map[int]bool))
	if err != nil {
		t.Fatal("Add service shouldn't return error here")
	}
	svc2, err := builder.AddService(config, 2, make(map[int]bool))
	if err != nil {
		t.Fatal("Add service shouldn't return error here")
	}
	assert.Assert(t, svc1 != svc2, "IDs should be different")
}

func TestDependencyBookkeeping(t *testing.T) {
	builder := NewServiceNetworkConfigBuilder()
	config := builder.AddTestImageConfiguration(getTestInitializerCore("test"), getTestCheckerCore())

	svc1, err := builder.AddService(config, 1, make(map[int]bool))
	if err != nil {
		t.Fatal("Add service shouldn't return error here")
	}

	svc2, err := builder.AddService(config, 2, make(map[int]bool))
	if err != nil {
		t.Fatal("Add service shouldn't return error here")
	}

	svc3Deps := map[int]bool{
		svc1: true,
		svc2: true,
	}
	svc3, err := builder.AddService(config, 3, svc3Deps)
	if err != nil {
		t.Fatal("Add service shouldn't return error here")
	}

	svc4Deps := map[int]bool{
		svc1: true,
		svc3: true,
	}
	svc4, err := builder.AddService(config, 4, svc4Deps)
	if err != nil {
		t.Fatal("Add service shouldn't return error here")
	}

	svc5Deps := map[int]bool{
		svc2: true,
	}
	svc5, err := builder.AddService(config, 5, svc5Deps)
	if err != nil {
		t.Fatal("Add service shouldn't return error here")
	}


	expectedOrder := []int{
		svc1,
		svc2,
		svc3,
		svc4,
		svc5,
	}
	assert.DeepEqual(t,
		expectedOrder,
		builder.servicesStartOrder)

	expectedDependents := map[int]bool{
		svc4: true,
		svc5: true,
	}
	if len(expectedDependents) != len(builder.onlyDependentServices) {
		t.Fatal("Size of dependent-only services didn't match expected")
	}
	for svcId := range builder.onlyDependentServices {
		if _, found := expectedDependents[svcId]; !found {
			t.Fatalf("ID %v should be marked as dependent-only, but wasn't", svcId)
		}
	}
}

func TestDefensiveCopies(t *testing.T) {
	builder := NewServiceNetworkConfigBuilder()
	config := builder.AddTestImageConfiguration(getTestInitializerCore("test1"), getTestCheckerCore())

	dependencyMap := make(map[int]bool)
	svc1, err := builder.AddService(config, 1, dependencyMap)
	if err != nil {
		t.Fatal("Add service shouldn't return error here")
	}

	networkConfig := builder.Build()

	_ = builder.AddTestImageConfiguration(getTestInitializerCore("test2"), getTestCheckerCore())
	_, err = builder.AddService(config, 2, make(map[int]bool))
	if err != nil {
		t.Fatal("Add service shouldn't return error here")
	}
	assert.Equal(t, 1, len(networkConfig.onlyDependentServices))
	assert.Equal(t, 1, len(networkConfig.serviceConfigs))
	assert.Equal(t, 1, len(networkConfig.servicesStartOrder))
	assert.Equal(t, 1, len(networkConfig.configurations))

	svcDependencies := networkConfig.serviceDependencies
	assert.Equal(t, 1, len(svcDependencies))
	dependencyMap[99] = true
	assert.Equal(t, 0, len(svcDependencies[svc1]))

	// TODO test that the dependencies in the GetStartCommand are what we expect!
}

func TestDependencyFeeding(t *testing.T) {
	builder := NewServiceNetworkConfigBuilder()
	service1Result := "test1"
	service1Initializer := getTestInitializerCore(service1Result)
	config1 := builder.AddTestImageConfiguration(service1Initializer, getTestCheckerCore())
	service2Result := "test2"
	service2Initializer := getTestInitializerCore(service2Result)
	config2 := builder.AddTestImageConfiguration(service2Initializer, getTestCheckerCore())
	service3Result := "test3"
	service3Initializer := getTestInitializerCore(service3Result)
	config3 := builder.AddTestImageConfiguration(service3Initializer, getTestCheckerCore())

	_, err := builder.AddService(config1, 1, make(map[int]bool))
	if err != nil {
		t.Fatal("Add service shouldn't return error here")
	}

	_, err = builder.AddService(config2, 2, map[int]bool{1: true})
	if err != nil {
		t.Fatal("Add service shouldn't return error here")
	}
	_, err = builder.AddService(config3, 3, map[int]bool{1: true, 2: true})
	if err != nil {
		t.Fatal("Add service shouldn't return error here")
	}
	networkConfig := builder.Build()
	networkConfig.CreateNetwork("testImage", )


	assert.Equal(t, 0, len(service1Initializer.GetLastDependencyResults()))
	service2DepResults := service2Initializer.GetLastDependencyResults()
	assert.Equal(t, 1, len(service2DepResults))
	if _, found := service2DepResults[service1Result]; !found {
		t.Fatalf("Should have found %v in service 2 dep results but it was missing", service1Result)
	}

	service3DepResults := service3Initializer.GetLastDependencyResults()
	assert.Equal(t, 2, len(service3DepResults))
	if _, found := service3DepResults[service1Result]; !found {
		t.Fatalf("Should have found %v in service 3 dep results but it was missing", service1Result)
	}
	if _, found := service3DepResults[service2Result]; !found {
		t.Fatalf("Should have found %v in service 3 dep results but it was missing", service2Result)
	}
}
