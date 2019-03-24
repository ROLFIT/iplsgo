// reader_test
package main

import (
	"flag"
	"gopkg.in/goracle.v1/oracle"
	"testing"
)

var (
	confDsn      = flag.String("conf-dsn", "", "Config Oracle DSN (user/passw@sid)")
	confDsnUser  string
	confDsnPassw string
	confDsnSID   string
)

func init() {
	flag.Parse()
	confDsnUser, confDsnPassw, confDsnSID = oracle.SplitDSN(*confDsn)
}

func TestReading(t *testing.T) {
	if !(*confDsn != "") {
		t.Fatalf("cannot test connection without dsn!")
	}
	err := startReading(*confDsn, "unionASR.xcfg", 1)
	if err != nil {
		t.Fatal(err)
	}
	stopReading()
	resetConfig()

	err = startReading(confDsnUser+"/"+confDsnPassw+"@"+confDsnSID, "__unionASR.xcfg", 1)
	if err == nil {
		stopReading()
		t.Fatal("Should be error")
	}
	resetConfig()
	err = startReading(confDsnUser+"/"+confDsnPassw+"invalid"+"@"+confDsnSID, "__unionASR.xcfg", 1)
	if err == nil {
		stopReading()
		t.Fatal("Should be error")
	}
	resetConfig()
}
