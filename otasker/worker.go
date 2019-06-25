//Package otasker ...
package otasker

import (
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/vsdutka/metrics"
	"github.com/vsdutka/mltpart"
)

var numberOfSessions = metrics.NewInt("PersistentHandler_Number_Of_Sessions", "Server - Number of persistent sessions", "Pieces", "p")

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

type worker struct {
	oracleTasker
	sync.RWMutex
	stopSignal   chan void
	stopCallback func(*worker)
}

func (w *worker) stop() {
	if w.stopCallback != nil {
		w.stopCallback(w)
	}
}

func (w *worker) listen(taskQueue <-chan *work, idleTimeout time.Duration) {
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
				w.oracleTasker = wrk.oraTaskerFactory()
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

type taskStatus struct {
	outChan   chan OracleTaskResult
	startTime time.Time
}

type managerTasks struct {
	manager         *Manager
	taskLock        sync.RWMutex
	tasksInProgress map[string]*taskStatus
}

type hr struct {
	path      string
	sessionID string
}

func (h *hr) AllWorkersGone(m *Manager) {
	wlock.Lock()
	delete(managersTasks[h.path], h.sessionID)
	wlock.Unlock()
}

func (h *hr) WorkerCreated(w *worker) {
	numberOfSessions.Add(1)
}

func (h *hr) WorkerStopped(w *worker) {
	numberOfSessions.Add(-1)
}

var (
	wlock         sync.RWMutex
	wlist         = make(map[string]map[string]*worker)
	managersTasks = make(map[string]map[string]*managerTasks)
)

const (
	//ClassicTasker соответствует методу NewOwaClassicProcTasker
	ClassicTasker = iota
	//ApexTasker соответствует методу NewOwaApexProcTasker
	ApexTasker
	//EkbTasker соответствует методу NewOwaEkbProcTasker
	EkbTasker
)

var (
	taskerFactory = map[int]func() oracleTasker{
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
	maxWorkerCount int,
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
	if maxWorkerCount < 0 {
		maxWorkerCount = 1
	}
	mTasks := func() *managerTasks {
		wlock.RLock()
		mTasks, ok := managersTasks[strings.ToUpper(path)][strings.ToUpper(sessionID)]
		wlock.RUnlock()
		if !ok {
			wlock.Lock()
			defer wlock.Unlock()
			hr := &hr{
				path:      path,
				sessionID: sessionID,
			}
			manager, _ := NewManager(hr, maxWorkerCount, idleTimeout)
			mTasks = &managerTasks{
				manager:         manager,
				tasksInProgress: make(map[string]*taskStatus),
			}
			if _, ok := managersTasks[strings.ToUpper(path)]; !ok {
				managersTasks[strings.ToUpper(path)] = make(map[string]*managerTasks)
			}
			managersTasks[strings.ToUpper(path)][strings.ToUpper(sessionID)] = mTasks
		}
		return mTasks
	}()
	// Проверяем, есть ли результаты по задаче
	mTasks.taskLock.RLock()
	taskStat, ok := mTasks.tasksInProgress[taskID]
	mTasks.taskLock.RUnlock()
	if !ok {
		taskStat = &taskStatus{
			outChan:   make(chan OracleTaskResult),
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
		allWorkersBusyTimeout, _ := mTasks.manager.AssignTask(wrk, maxWorkerCount, waitTimeout)
		if allWorkersBusyTimeout {
			taskDuration := time.Duration(0)
			mTasks.taskLock.RLock()
			//Если в справочнике исполняемых ровно одно задание,..
			if len(mTasks.tasksInProgress) == 1 {
				//..то будем отсчитывать длительность от его начала
				for _, taskStat := range mTasks.tasksInProgress {
					taskDuration = time.Since(taskStat.startTime)
				}
			}
			mTasks.taskLock.RUnlock()
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
				mTasks.taskLock.Lock()
				delete(mTasks.tasksInProgress, taskID)
				mTasks.taskLock.Unlock()
			}
			return res
		case <-time.After(waitTimeout):
			{
				//Если задания нет в справочнике исполняемых,..
				if !ok {
					//..то добавим его в справочник
					mTasks.taskLock.Lock()
					mTasks.tasksInProgress[taskID] = taskStat
					mTasks.taskLock.Unlock()
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
	mTasks, ok := managersTasks[strings.ToUpper(path)][strings.ToUpper(sessionID)]
	wlock.RUnlock()
	if !ok {
		return nil
	}
	return mTasks.manager.BreakAll()
}
