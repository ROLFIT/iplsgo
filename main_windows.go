package main

//go:generate C:\!Dev\GOPATH\src\github.com\rolfit\gover\gover.exe
import (
	"flag"
	"fmt"
	"log"
	"os"
	"runtime"
	"sync"
	"time"

	"github.com/kardianos/service"
	//_ "golang.org/x/tools/go/ssa"
	"gopkg.in/goracle.v1/oracle"
)

/*
	ВАЖНО собирать с set
	set GODEBUG=cgocheck=0
	set CGO_CFLAGS=-ID:\oracle\instantclient_12_1\sdk\include
	set CGO_LDFLAGS=-LD:\oracle\instantclient_12_1\sdk\lib -loci
*/
var (
	logger     service.Logger
	loggerLock sync.Mutex
	svcFlag    *string
)

func logInfof(format string, a ...interface{}) error {
	loggerLock.Lock()
	defer loggerLock.Unlock()
	if logger != nil {
		return logger.Infof(format, a...)
	}
	return nil
}

func logError(v ...interface{}) error {
	loggerLock.Lock()
	defer loggerLock.Unlock()
	if logger != nil {
		return logger.Error(v)
	}
	return nil
}

// Program structures.
//  Define Start and Stop methods.
type program struct {
	exit chan struct{}
}

func (p *program) Start(s service.Service) error {
	if service.Interactive() {
		logInfof("Service \"%s\" is running in terminal.", confServiceDispName)
	} else {
		logInfof("Service \"%s\" is running under service manager.", confServiceDispName)
	}
	p.exit = make(chan struct{})

	// Start should not block. Do the actual work async.
	go p.run()
	return nil
}

func (p *program) run() {
	startServer()
	logInfof("Service \"%s\" is started.", confServiceDispName)
	for {
		select {
		case <-p.exit:
			return
		}
	}
}

func (p *program) Stop(s service.Service) error {
	// Any work in Stop should be quick, usually a few seconds at most.
	logInfof("Service \"%s\" is stopping.", confServiceDispName)
	stopReading()
	stopServer()
	logInfof("Service \"%s\" is stopped.", confServiceDispName)
	close(p.exit)
	return nil
}

// Service setup.
//   Define service config.
//   Create the service.
//   Setup the logger.
//   Handle service controls (optional).
//   Run the service.
func main() {
	runtime.GOMAXPROCS(runtime.NumCPU())
	oracle.IsDebug = false

	setupFlags()
	svcFlag = flag.String("service", "", fmt.Sprintf("Control the system service. Valid actions: %q\n", service.ControlAction))
	flag.Parse()

	if *verFlag == true {
		fmt.Println("Version: ", Version)
		fmt.Println("Build:   ", BuildDate)
		os.Exit(0)
	}

	if (*confNameFlag == "") || (*dsnFlag == "") {
		usage()
		os.Exit(2)
	}

	err := startReading(*dsnFlag, *confNameFlag, (time.Duration)(*confReadTimeoutFlag)*time.Second)
	if err != nil {
		log.Fatal(err)
	}

	svcConfig := &service.Config{
		Name:        confServiceName,
		DisplayName: confServiceDispName,
		Description: confServiceDispName,
		Arguments: []string{fmt.Sprintf("-dsn=%s", *dsnFlag),
			fmt.Sprintf("-conf=%s", *confNameFlag),
			fmt.Sprintf("-conf_tm=%v", *confReadTimeoutFlag),
			fmt.Sprintf("-host=%v", *hostFlag),
		},
	}

	prg := &program{}
	s, err := service.New(prg, svcConfig)
	if err != nil {
		log.Fatal(err)
	}

	errs := make(chan error, 5)
	func() {
		loggerLock.Lock()
		defer loggerLock.Unlock()
		logger, err = s.Logger(errs)
		if err != nil {
			log.Fatal(err)
		}
	}()

	go func() {
		for {
			err := <-errs
			if err != nil {
				log.Print(err)
			}
		}
	}()

	if len(*svcFlag) != 0 {
		err := service.Control(s, *svcFlag)
		if err != nil {
			log.Printf("Valid actions: %q\n", service.ControlAction)
			log.Fatal(err)
		}
		return
	}

	err = s.Run()
	if err != nil {
		logError(err)
	}
}
