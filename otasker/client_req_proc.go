//Package otasker ...
package otasker

import (
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/rolfit/metrics"
	"github.com/rolfit/mltpart"
)

var numberOfSessions = metrics.NewInt("PersistentHandler_Number_Of_Sessions", "Server - Number of persistent sessions", "Pieces", "p")

const (
	//ClassicTasker соответствует методу NewOwaClassicProcTasker
	ClassicTasker = iota
	//ApexTasker соответствует методу NewOwaApexProcTasker
	ApexTasker
	//EkbTasker соответствует методу NewOwaEkbProcTasker
	EkbTasker
)

var (
	wlock            sync.RWMutex
	dispatchersTasks = make(map[string]map[string]*dispatcherTasks)
	taskerFactory    = map[int]func() oracleTasker{
		ClassicTasker: NewOwaClassicProcTasker(),
		ApexTasker:    NewOwaApexProcTasker(),
		EkbTasker:     NewOwaEkbProcTasker(),
	}
)

//Run выполняет задание, формируемое из входных параметров
//с помощью доступных исполнителей и возвращает результат
func Run(
	path string,
	typeTasker int,
	maxProcCount int,
	sessionID,
	taskID,
	userName,
	userPass,
	connStr,
	paramStoreProc,
	beforeScript,
	afterScript,
	documentTable string,
	cgiEnv map[string]string,
	procName string,
	urlParams url.Values,
	reqFiles *mltpart.Form,
	waitTimeout, idleTimeout time.Duration,
	dumpFileName string,
) OracleTaskResult {
	if maxProcCount < 0 {
		maxProcCount = 1
	}
	dTasks := func() *dispatcherTasks {
		wlock.RLock()
		dTasks, ok := dispatchersTasks[strings.ToUpper(path)][strings.ToUpper(sessionID)]
		wlock.RUnlock()
		if !ok {
			wlock.Lock()
			defer wlock.Unlock()
			hr := &hr{
				path:      path,
				sessionID: sessionID,
			}
			dispatcher, _ := NewDispatcher(hr, maxProcCount, idleTimeout)
			dTasks = &dispatcherTasks{
				dispatcher:      dispatcher,
				tasksInProgress: make(map[string]*taskStatus),
			}
			if _, ok := dispatchersTasks[strings.ToUpper(path)]; !ok {
				dispatchersTasks[strings.ToUpper(path)] = make(map[string]*dispatcherTasks)
			}
			dispatchersTasks[strings.ToUpper(path)][strings.ToUpper(sessionID)] = dTasks
		}
		return dTasks
	}()
	// Проверяем, есть ли результаты по задаче
	dTasks.taskLock.RLock()
	taskStat, ok := dTasks.tasksInProgress[taskID]
	dTasks.taskLock.RUnlock()
	if !ok {
		taskStat = &taskStatus{
			// В первую очередь буфер в канале нужен в следующем сценарии:
			// 1. Запускаем "долгоиграющий" запрос, который приводит к выходу
			//    из ожидания результата по таймауту
			// 2. Закрываем окно c запросом
			// 3. Убиваем сессию из другого окна
			// 4. У воркера должна быть возможность завершить работу, хотя
			//    результат читать уже некому
			outChan:   make(chan OracleTaskResult, 1),
			startTime: time.Now(),
		}
		wrk := &work{
			sessionID:         sessionID,
			taskID:            taskID,
			oraTaskerFactory:  taskerFactory[typeTasker],
			outChan:           taskStat.outChan,
			reqUserName:       userName,
			reqUserPass:       userPass,
			reqConnStr:        connStr,
			reqParamStoreProc: paramStoreProc,
			reqBeforeScript:   beforeScript,
			reqAfterScript:    afterScript,
			reqDocumentTable:  documentTable,
			reqCGIEnv:         cgiEnv,
			reqProc:           procName,
			reqParams:         urlParams,
			reqFiles:          reqFiles,
			dumpFileName:      dumpFileName,
		}
		allWorkersBusyTimeout, _ := dTasks.dispatcher.AssignTask(wrk, maxProcCount, waitTimeout)
		if allWorkersBusyTimeout {
			taskDuration := time.Duration(0)
			dTasks.taskLock.RLock()
			//Если в справочнике исполняемых ровно одно задание,..
			if len(dTasks.tasksInProgress) == 1 {
				//..то будем отсчитывать длительность от его начала
				for _, taskStat := range dTasks.tasksInProgress {
					taskDuration = time.Since(taskStat.startTime)
				}
			}
			dTasks.taskLock.RUnlock()
			taskDurationSeconds := int64(taskDuration / time.Second)
			// Сигнализируем о том, что идет выполнение другог запроса и нужно предложить прервать
			return OracleTaskResult{StatusCode: StatusBreakPage, Duration: taskDurationSeconds}
		}
	}
	//Читаем результаты
	return func() OracleTaskResult {
		select {
		case res := <-taskStat.outChan:
			//Задание завершено
			//Если задание есть в справочнике исполняемых,..
			if ok {
				//..то удалим его из справочника
				dTasks.taskLock.Lock()
				delete(dTasks.tasksInProgress, taskID)
				dTasks.taskLock.Unlock()
			}
			return res
		case <-time.After(waitTimeout):
			{
				//Если задания нет в справочнике исполняемых,..
				if !ok {
					//..то добавим его в справочник
					dTasks.taskLock.Lock()
					dTasks.tasksInProgress[taskID] = taskStat
					dTasks.taskLock.Unlock()
				}
				taskDuration := time.Since(taskStat.startTime)
				taskDurationSeconds := int64(taskDuration / time.Second)
				// Сигнализируем о том, что идет выполнение этого запроса и нужно показать червяка
				return OracleTaskResult{StatusCode: StatusWaitPage, Duration: taskDurationSeconds}
			}
		}
	}()
}

//Break останавливает всех исполнителей пользователя path|sessionID
func Break(path, sessionID string) error {
	wlock.RLock()
	dTasks, ok := dispatchersTasks[strings.ToUpper(path)][strings.ToUpper(sessionID)]
	wlock.RUnlock()
	if !ok {
		return nil
	}
	return dTasks.dispatcher.BreakAll()
}

type work struct {
	sessionID         string
	taskID            string
	oraTaskerFactory  func() oracleTasker
	outChan           chan<- OracleTaskResult
	reqUserName       string
	reqUserPass       string
	reqConnStr        string
	reqParamStoreProc string
	reqBeforeScript   string
	reqAfterScript    string
	reqDocumentTable  string
	reqCGIEnv         map[string]string
	reqProc           string
	reqParams         url.Values
	reqFiles          *mltpart.Form
	dumpFileName      string
}

type clientReqProc struct {
	oracleTasker
	sync.RWMutex
	stopSignal   chan void
	stopCallback func(*clientReqProc)
}

func (w *clientReqProc) stop() {
	if w.stopCallback != nil {
		w.stopCallback(w)
	}
}

func (w *clientReqProc) listen(taskQueue <-chan *work, idleTimeout time.Duration) {
	defer func() {
		w.stop()
		// Удаляем данный обработчик из списка доступных
		w.CloseAndFree()
	}()
	for {
		select {
		case <-w.stopSignal:
			{
				return
			}
		case wrk := <-taskQueue:
			{
				outChan := wrk.outChan
				if outChan == nil {
					continue
				}
				if w.oracleTasker.conn == nil {
					w.oracleTasker = wrk.oraTaskerFactory()
				}
				res := w.Run(wrk.sessionID,
					wrk.taskID,
					wrk.reqUserName,
					wrk.reqUserPass,
					wrk.reqConnStr,
					wrk.reqParamStoreProc,
					wrk.reqBeforeScript,
					wrk.reqAfterScript,
					wrk.reqDocumentTable,
					wrk.reqCGIEnv,
					wrk.reqProc,
					wrk.reqParams,
					wrk.reqFiles,
					wrk.dumpFileName)
				outChan <- res
				if res.StatusCode == StatusRequestWasInterrupted {
					return
				}
				select {
				case <-w.stopSignal:
					{
						return
					}
				default:
				}
			}
		case <-time.After(idleTimeout):
			{
				return
			}
		}
	}
}

type dispatcherTasks struct {
	dispatcher      *Dispatcher
	taskLock        sync.RWMutex
	tasksInProgress map[string]*taskStatus
}

type taskStatus struct {
	outChan   chan OracleTaskResult
	startTime time.Time
}

type hr struct {
	path      string
	sessionID string
}

func (h *hr) AllProcessorsStopped(d *Dispatcher) {
	wlock.Lock()
	delete(dispatchersTasks[h.path], h.sessionID)
	wlock.Unlock()
}

func (h *hr) ProcessorCreated(w *clientReqProc) {
	numberOfSessions.Add(1)
}

func (h *hr) ProcessorStopped(w *clientReqProc) {
	numberOfSessions.Add(-1)
}
