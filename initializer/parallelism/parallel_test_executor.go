package parallelism

// TODO TODO TODO Combine this entire file into test_suite_runner????

import (
	"context"
	"github.com/docker/distribution/uuid"
	"github.com/docker/docker/client"
	"github.com/sirupsen/logrus"
	"os"
	"sync"
)

// ================= Test params & result ============================
type ParallelTestParams struct {
	TestName            string
	LogFp               *os.File
	SubnetMask          string
	ExecutionInstanceId uuid.UUID
}

type ParallelTestOutput struct {
	TestName     string
	ExecutionErr error    // Indicates whether an error occurred during the execution of the test that prevented it from running
	TestPassed   bool     // Indicates whether the test passed or failed (undefined if the test had a setup error)
	LogFp        *os.File // Where the logs for this test got written to
}

// ================= Parallel executor ============================
type ParallelTestExecutor struct {
	executionId uuid.UUID
	dockerClient *client.Client
	testControllerImageName string
	testControllerLogLevel string
	testServiceImageName string
	parallelism int
}

/*
Creates a new ParallelTestExecutor which will run tests in parallel using the given parameters.

Args:
	executionId: The UUID uniquely identifying this execution of the tests
	dockerClient: The handle to manipulating the Docker environment
	testControllerImageName: The name of the Docker image that will be used to run the test controller
	testServiceImageName: The name of the Docker image of the version of the service being tested
	parallelism: The number of tests to run concurrently
 */
func NewParallelTestExecutor(
			executionId uuid.UUID,
			dockerClient *client.Client,
			testControllerImageName string,
			testControllerLogLevel string,
			testServiceImageName string,
			parallelism int) *ParallelTestExecutor {
	return &ParallelTestExecutor{
		executionId: executionId,
		dockerClient: dockerClient,
		testControllerImageName: testControllerImageName,
		testControllerLogLevel: testControllerLogLevel,
		testServiceImageName: testServiceImageName,
		parallelism: parallelism,
	}
}

func (executor ParallelTestExecutor) RunTestsInParallel(tests map[string]ParallelTestParams) map[string]ParallelTestOutput {
	// These need to be buffered else sending to the channel will be blocking
	testParamsChan := make(chan ParallelTestParams, len(tests))
	testOutputChan := make(chan ParallelTestOutput, len(tests))

	logrus.Info("Loading test params into work queue...")
	for _, testParams := range tests {
		testParamsChan <- testParams
	}
	close(testParamsChan)
	logrus.Info("All test params loaded into work queue")

	logrus.Infof("Launching %v tests with parallelism %v...", len(tests), executor.parallelism)
	executor.disableSystemLogAndRunTestThreads(testParamsChan, testOutputChan)
	logrus.Info("All tests exited")

	// Collect all results
	result := make(map[string]ParallelTestOutput)
	for i := 0; i < len(tests); i++ {
		output := <-testOutputChan
		result[output.TestName] = output
	}
	return result
}

func (executor ParallelTestExecutor) disableSystemLogAndRunTestThreads(testParamsChan chan ParallelTestParams, testOutputChan chan ParallelTestOutput) {
	/*
    Because each test needs to have its logs written to an independent file to avoid getting logs all mixed up, we need to make
    sure that all code below this point uses the per-test logger rather than the systemwide logger. However, it's very difficult for
    a coder to remember to use 'log.Info' when they're used to doing 'logrus.Info'. To enforce this, we make the systemwide logger throw
	a panic during just this function call.
	*/
	logrus.SetOutput(panickingLogWriter{})
	defer logrus.SetOutput(os.Stdout)

	var waitGroup sync.WaitGroup
	for i := 0; i < executor.parallelism; i++ {
		waitGroup.Add(1)
		go executor.runTestWorker(&waitGroup, testParamsChan, testOutputChan)
	}

	// TODO add a timeout which the total execution must not exceed
	waitGroup.Wait()
}

/*
A function, designed to be run inside a goroutine, that will pull from the given test params channel, execute a test, and
push the result to the test results channel
 */
func (executor ParallelTestExecutor) runTestWorker(
			waitGroup *sync.WaitGroup,
			testParamsChan chan ParallelTestParams,
			testOutputChan chan ParallelTestOutput) {
	// IMPORTANT: make sure that we mark a thread as done!
	defer waitGroup.Done()

	for testParams := range testParamsChan {
		// Create a separate logger just for this test that writes to the given file
		log := logrus.New()
		log.SetLevel(logrus.GetLevel())
		log.SetOutput(testParams.LogFp)
		log.SetFormatter(logrus.StandardLogger().Formatter)

		loggedExecutor := newLoggedTestExecutor(log)

		// TODO create a new context for the test itself probably, so we can cancel it if it's running too long!
		testContext := context.Background()

		passed, executionErr := loggedExecutor.runTest(
			executor.executionId,
			testContext,
			executor.dockerClient,
			testParams.SubnetMask,
			executor.testControllerImageName,
			executor.testControllerLogLevel,
			executor.testServiceImageName,
			testParams.TestName)

		result := ParallelTestOutput{
			TestName:     testParams.TestName,
			ExecutionErr: executionErr,
			TestPassed:   passed,
			LogFp:        testParams.LogFp,
		}
		testOutputChan <- result
	}
}