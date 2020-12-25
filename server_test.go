// server_test
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rolfit/iplsgo/otasker"
	"gopkg.in/errgo.v1"
	"gopkg.in/goracle.v1/oracle"
)

type VD struct {
	Path               string `json:"Path"`
	Type               string `json:"Type"`
	RootDir            string `json:"RootDir"`
	RedirectPath       string `json:"RedirectPath"`
	SessionIdleTimeout int    `json:"owa.SessionIdleTimeout"`
	SessionWaitTimeout int    `json:"owa.SessionWaitTimeout"`
	RequestUserInfo    bool   `json:"owa.ReqUserInfo"`
	RequestUserRealm   string `json:"owa.ReqUserRealm"`
	DefUserName        string `json:"owa.DBUserName"`
	DefUserPass        string `json:"owa.DBUserPass"`
	BeforeScript       string `json:"owa.BeforeScript"`
	AfterScript        string `json:"owa.AfterScript"`
	ParamStoreProc     string `json:"owa.ParamStroreProc"`
	DocumentTable      string `json:"owa.DocumentTable"`
	Templates          []struct {
		Code string
		Body string
	} `json:"owa.Templates"`
	Grps []struct {
		ID  int32
		SID string
	} `json:"owa.UserGroups"`
	SoapUserName string `json:"soap.DBUserName"`
	SoapUserPass string `json:"soap.DBUserPass"`
	SoapConnStr  string `json:"soap.DBConnStr"`
}

type sc struct {
	ServiceName      string `json:"Service.Name"`
	ServiceDispName  string `json:"Service.DisplayName"`
	HTTPPort         int    `json:"Http.Port"`
	HTTPDebugPort    int    `json:"Http.DebugPort"`
	HTTPReadTimeout  int    `json:"Http.ReadTimeout"`
	HTTPWriteTimeout int    `json:"Http.WriteTimeout"`
	HTTPSsl          bool   `json:"Http.SSL"`
	HTTPSslCert      string `json:"Http.SSLCert"`
	HTTPSslKey       string `json:"Http.SSLKey"`
	HTTPLogDir       string `json:"Http.LogDir"`
	HTTPUsers        []struct {
		Name      string
		IsSpecial bool
		GRP_ID    int32
	} `json:"Http.Users"`
	Handlers []VD `json:"Http.Handlers"`
}

var (
	testDsn      = flag.String("test-dsn", "", "Test Oracle DSN (user/passw@sid)")
	testDsnUser  string
	testDsnPassw string
	testDsnSID   string
	serverConf   sc
)

var _ = func() bool {
	testing.Init()
	return true
}()

func init() {
	flag.Parse()
	testDsnUser, testDsnPassw, testDsnSID = oracle.SplitDSN(*testDsn)
	serverConf = sc{
		ServiceName:      "ServiceName",
		ServiceDispName:  "ServiceDispName",
		HTTPPort:         9977,
		HTTPDebugPort:    8877,
		HTTPReadTimeout:  999,
		HTTPWriteTimeout: 888,
		HTTPSsl:          false,
		HTTPSslCert:      "HTTPSslCert",
		HTTPSslKey:       "HTTPSslKey",
		HTTPLogDir:       "${app_dir}\\log\\",
		HTTPUsers: []struct {
			Name      string
			IsSpecial bool
			GRP_ID    int32
		}{
			struct {
				Name      string
				IsSpecial bool
				GRP_ID    int32
			}{testDsnUser, false, 1},
			struct {
				Name      string
				IsSpecial bool
				GRP_ID    int32
			}{"USER001", false, 1},
		},
		Handlers: []VD{
			VD{
				Path:    "/Images",
				Type:    "Static",
				RootDir: "D:\\wwwroot\\Images\\",
			},
			VD{
				Path:         "/",
				Type:         "Redirect",
				RedirectPath: "/images",
			},
			VD{
				Path:               "/ti8",
				Type:               "owa_classic",
				SessionIdleTimeout: 30000,
				SessionWaitTimeout: 10000,
				RequestUserInfo:    true,
				RequestUserRealm:   "/ti8",
				DefUserName:        "",
				DefUserPass:        "",
				BeforeScript:       "session_init.init;",
				AfterScript:        "",
				ParamStoreProc:     "wex.ws",
				DocumentTable:      "wwv_document",
				Templates: []struct {
					Code string
					Body string
				}{
					{"error", "{{.ErrMsg}}"},
				},
				Grps: []struct {
					ID  int32
					SID string
				}{
					struct {
						ID  int32
						SID string
					}{1, testDsnSID},
				},
			},
			VD{
				Path:               "/ti8_a",
				Type:               "owa_classic",
				SessionIdleTimeout: 30000,
				SessionWaitTimeout: 10000,
				RequestUserInfo:    false,
				RequestUserRealm:   "/ti8_a",
				DefUserName:        testDsnUser,
				DefUserPass:        testDsnPassw,
				BeforeScript:       "session_init.init;",
				AfterScript:        "",
				ParamStoreProc:     "wex.ws",
				DocumentTable:      "wwv_document",
				Templates: []struct {
					Code string
					Body string
				}{
					{"error", "{{.ErrMsg}}"},
				},
				Grps: []struct {
					ID  int32
					SID string
				}{
					struct {
						ID  int32
						SID string
					}{1, testDsnSID},
				},
			},
			VD{
				Path:         "/soap",
				Type:         "SOAP",
				SoapUserName: testDsnUser,
				SoapUserPass: testDsnPassw,
				SoapConnStr:  testDsnSID,
			},
		},
	}
}

func exec(stm string) error {
	if !(*testDsn != "") {
		return errgo.New("cannot test connection without dsn!")
	}

	var err error
	conn, err := oracle.NewConnection(testDsnUser, testDsnPassw, testDsnSID, false)
	if err != nil {
		return errgo.New("cannot create connection: " + err.Error())
	}
	if err = conn.Connect(0, false); err != nil {
		return errgo.New("error connecting: " + err.Error())
	}
	defer conn.Close()
	cur := conn.NewCursor()
	defer cur.Close()
	return cur.Execute(stm, nil, nil)
}

func performRequest(t *testing.T, method, username, password, urlStr, body, response string, responseCode int) {
	req, _ := http.NewRequest(method, urlStr, bytes.NewReader([]byte(body)))
	if username != "" {
		req.SetBasicAuth(username, password)
	}
	if method == "POST" {
		req.Header.Add("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Add("Content-Length", strconv.Itoa(len(body)))
	}

	w := httptest.NewRecorder()

	serveHTTP(w, req)

	if w.Code != responseCode {
		t.Errorf("Method %s Url \"%s\" Status code should be %v, was %d", method, urlStr, responseCode, w.Code)
	}
	res := strings.Replace(w.Body.String(), "\n", "", -1)
	if res != response {
		t.Errorf("Method %s Url \"%s\" Response should be \"%s\", was \"%s\"", method, urlStr, response, res)
	}
}

const (
	stmCreatePrc = `
create or replace procedure %s(ap in %s) is 
begin
  htp.set_ContentType('text/plain');
  htp.add_CustomHeader('CUSTOM_HEADER: HEADER
CUSTOM_HEADER1: HEADER1
');
  htp.prn(ap);
  hrslt.ADD_FOOTER := false;
  rollback;
end;`
)

func TestServe(t *testing.T) {
	var data = url.Values{
		"ap": []string{"Тестовое%29 сообщение!!!"},
	}
	var tests = []struct {
		method       string
		urlStr       string
		username     string
		password     string
		body         string
		response     string
		responseCode int
	}{
		{"GET", "/Images/dir/100.html", "", "", "", "100", http.StatusOK},
		{"GET", "/images/dir/100.html", "", "", "", "100", http.StatusOK},
		{"GET", "/images/100.html?afsdfsfq4fwer", "", "", "", "100", http.StatusOK},
		{"GET", "/images/dir/100.html", "", "", "", "100", http.StatusOK},
		{"GET", "/images/dir/", "", "", "", "<pre><a href=\"100.html\">100.html</a></pre>", http.StatusOK},
		{"GET", "/", "", "", "", "<a href=\"/images\">Moved Permanently</a>.", http.StatusMovedPermanently},
		{"GET", "/ti8_a/a.server_test?ap=1", "", "", "", "1", http.StatusOK},
		{"GET", "/ti8_a/sfsfsf/a.server_test?ap=1", "", "", "", "1", http.StatusOK},
		{"POST", "/ti8/a.server_test", testDsnUser, testDsnPassw, data.Encode(), data.Get("ap"), http.StatusOK},
		{"POST", "/tI8/a.server_test", testDsnUser, testDsnPassw, data.Encode(), data.Get("ap"), http.StatusOK},
		{"GET", "/debug/conf/users", "", "", "", "{\"A\":{\"IsSpecial\":false,\"GrpID\":1},\"USER001\":{\"IsSpecial\":false,\"GrpID\":1}}", http.StatusOK},
	}

	buf, err := json.Marshal(serverConf)
	if err != nil {
		t.Fatal(err)
	}
	resetConfig()
	err = parseConfig(buf)
	if err != nil {
		t.Fatal(err)
	}

	err = exec(fmt.Sprintf(stmCreatePrc, "server_test", "varchar2"))
	if err != nil {
		t.Fatalf("%s - Error when create procedure \"%s\": %s", "varchar2", "server_test", err.Error())
	}

	for _, v := range tests {
		performRequest(t, v.method, v.username, v.password, v.urlStr, v.body, v.response, v.responseCode)
	}
	err = exec("drop procedure server_test")
	if err != nil {
		t.Fatalf("%s - Error when drop procedure \"%s\": %s", "varchar2", "server_test", err.Error())
	}
}

func TestExpandFileName(t *testing.T) {
	resetConfig()
	var tests = []struct {
		srcStr string
		resStr string
	}{
		{"${APP_DIR}", basePath},
		{"${LOG_DIR}", basePath + "\\log\\"},
		{"${SERVICE_NAME}", fmt.Sprintf("%s_%d", serverConf.ServiceName, serverConf.HTTPPort)},
		{"${DATE}", time.Now().Format("2006_01_02")},
	}

	buf, err := json.Marshal(serverConf)
	if err != nil {
		t.Fatal(err)
	}
	err = parseConfig(buf)
	if err != nil {
		t.Fatal(err)
	}
	err = parseConfig(buf)
	if err != nil {
		t.Fatal(err)
	}

	for _, v := range tests {
		res := expandFileName(v.srcStr)
		if res != v.resStr {
			t.Errorf("Response should be \"%s\", was \"%s\"", v.resStr, res)
		}
	}
}

//BenchmarkLeakingOnTabClosedAfterTimeout проверяет наличие утечки памяти веб-сервера в ситуации
//когда был достигнут таймаут выполнения задания и дальше была закрыта вкладка с заданием
func BenchmarkLeakingOnTabClosedAfterTimeout(b *testing.B) {
	/*
		/ Запуск бенчмарка:
		/ go test -v -run=XXX -bench=LeakingOnTabClosedAfterTimeout -benchtime 10x -memprofile memprofile.out -args -test-dsn=a/aaa111@dp-se-tst17
		/
		/ После того, как он отработает необходимо проанализировать собранный профиль памяти -- там не должно быть большого количества объектов
		/ Для анализа выполнить
		/ go tool pprof memprofile.out
		/ И смотреть inuse_space
	*/
	//Настройка окружения для теста
	buf, err := json.Marshal(serverConf)
	if err != nil {
		b.Fatal(err)
	}
	resetConfig()
	err = parseConfig(buf)
	if err != nil {
		b.Fatal(err)
	}

	err = exec(`begin
	insert into a.UA_ACD4ORA_EXEC_OBJ(Object_Name, Acd_Id) select upper('mem_leak_test'), acd_id from a.UA_ACD4ORA_EXEC_OBJ t where t.object_name = upper('root$');
	commit;
	end;`)
	if err != nil {
		b.Fatal("Error when create UA_ACD4ORA_EXEC_OBJ", err.Error())
	}
	err = exec(`create or replace procedure a.mem_leak_test(p_duration in integer) is 
	begin
	  dbms_lock.sleep(p_duration);
	  a.hrslt.ADD_FOOTER := false;
	  for i in 1..10000 loop
		htp.p('alskdbf134u5h0143oasdfn14395013asdfnJKFJDFB91hKJSFiuh((#*SIFBADFHBUS*34asdfbiba18430asidfiasjdfaas))');
	  end loop;
	end mem_leak_test;`)
	if err != nil {
		b.Fatal("Error when create procedure mem_leak_test", err.Error())
	}
	err = exec(`create or replace public synonym mem_leak_test for a.mem_leak_test`)
	if err != nil {
		b.Fatal("Error when create public synonym", err.Error())
	}

	const url = "/ti8_a/mem_leak_test?p_duration=13"
	req, _ := http.NewRequest("GET", url, nil)

	w := httptest.NewRecorder()
	otasker.KillTimerDelay = time.Second * 10
	b.ResetTimer()

	//Тест
	for i := 0; i < b.N; i++ {
		//mem_leak_test печатает 10Мб текста через время p_duration
		//Если время p_duration превышает таймаут, то результат будет
		//получен после первого возврата по таймауту (червяка)
		serveHTTP(w, req)

		if w.Code != http.StatusOK {
			b.Errorf("Method %s Url \"%s\" Status code should be %v, was %d", "GET", url, http.StatusOK, w.Code)
		}
	}

	//Чистка мусора после теста
	err = exec("drop procedure a.mem_leak_test")
	if err != nil {
		b.Fatal("Error when drop procedure mem_leak_test", err.Error())
	}
	err = exec(`begin
	delete from a.UA_ACD4ORA_EXEC_OBJ where Object_Name = upper('mem_leak_test');
	commit;
	end;`)
	if err != nil {
		b.Fatal("Error when delete data for mem_leak_test", err.Error())
	}
	//Подождём время для очистки всех незатребованных результатов
	time.Sleep(otasker.KillTimerDelay * 2)
}
