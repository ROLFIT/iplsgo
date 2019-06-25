package otasker

import (
	"fmt"
	"sync"
	"time"
)

type void struct{}

var signal void

// Manager позволяет распределять работу на несколько
// исполнителей
type Manager struct {
	taskQueue      chan *work
	vacancyChanged chan void
	vacancies      chan void
	maxWorkerCount int
	workers        map[*worker]void
	vacanciesLock  sync.RWMutex
	workerLifespan time.Duration
	hr             *hr
}

//NewManager возвращает новый инициализированный объект Manager
func NewManager(hr *hr, maxWorkerCount int, workerLifespan time.Duration) (*Manager, error) {
	if maxWorkerCount < 0 {
		return nil, fmt.Errorf("Negative value %v for maxWorkerCount is not allowed", maxWorkerCount)
	}
	if workerLifespan <= 0 {
		return nil, fmt.Errorf("Nonpositive value %v for workerLifespan is not allowed", workerLifespan)
	}
	manager := &Manager{
		taskQueue:      make(chan *work),
		vacancyChanged: make(chan void),
		workers:        make(map[*worker]void),
		workerLifespan: workerLifespan,
		hr:             hr,
	}
	manager.setupVacancies(maxWorkerCount, true)
	return manager, nil
}

//AssignTask распределяет работу между несколькими исполнителями
//Возвращает флаг таймаута поиска исполнителя и ошибку
func (m *Manager) AssignTask(request *work, maxWorkerCount int, timeout time.Duration) (bool, error) {
	if maxWorkerCount < 0 {
		return false, fmt.Errorf("Negative value %v for maxWorkerCount is not allowed", maxWorkerCount)
	}
	m.setupVacancies(maxWorkerCount, false)

	vacancies := m.vacancies
	for {
		select {
		case m.taskQueue <- request:
			{
				//Задача назначена свободному исполнителю
				//Обновим вакансии и завершим работу менеджера
				//по обработке запроса
				vacancies = m.vacancies
				return false, nil
			}
		case <-m.vacancyChanged:
			{
				//Обновим список вакансий
				vacancies = m.vacancies
			}
		case <-vacancies:
			{
				//Свободных исполнителей нет, но есть вакансии
				//создадим нового исполнителя
				m.createWorker()
			}
		case <-time.After(timeout):
			{
				//Исполнитель не был назначен в течение отведённого времени
				return true, nil
			}
		}
	}
}

//BreakAll останавливает всех исполнителей
func (m *Manager) BreakAll() error {
	m.vacanciesLock.Lock()
	defer m.vacanciesLock.Unlock()
	for worker := range m.workers {
		select {
		case worker.stopSignal <- signal:
			{
				return nil
			}
		default:
			{
				// Воркер занят. Прерываем его
				return worker.Break()
			}
		}
	}
	return nil
}

//createWorker создаёт нового исполнителя и регистрирует
//его в справочнике исполнителей workers
func (m *Manager) createWorker() {
	workerStopped := func(w *worker) {
		m.vacanciesLock.Lock()
		delete(m.workers, w)
		if m.maxWorkerCount > len(m.workers) {
			m.vacancies <- signal
		}
		if m.hr != nil {
			m.hr.WorkerStopped(w)
			if len(m.workers) == 0 {
				m.hr.AllWorkersGone(m)
			}
		}
		m.vacanciesLock.Unlock()
	}
	m.vacanciesLock.Lock()
	defer m.vacanciesLock.Unlock()
	worker := &worker{
		stopSignal:   make(chan void, 1),
		stopCallback: workerStopped,
	}
	m.workers[worker] = signal
	if m.isInfiniteWorkersAllowed() {
		m.vacancies <- signal
	}
	go worker.listen(m.taskQueue, m.workerLifespan)
	if m.hr != nil {
		m.hr.WorkerCreated(worker)
	}
}

//setupVacancies управляет количеством текущих вакансий
func (m *Manager) setupVacancies(maxWorkerCount int, isInit bool) {
	m.vacanciesLock.Lock()
	defer m.vacanciesLock.Unlock()
	if !isInit && m.maxWorkerCount == maxWorkerCount {
		return
	}
	m.maxWorkerCount = maxWorkerCount

	if !m.isInfiniteWorkersAllowed() {
		currentWorkersCount := len(m.workers)
		m.vacancies = make(chan void, maxWorkerCount)
		//Создадим в новом канале вакансий вакансии в количестве maxWorkerCount - currentWorkersCount
		for i := currentWorkersCount; i < maxWorkerCount; i++ {
			m.vacancies <- signal
		}
		i := 0
		//Пошлём "лишним" исполнителям сигнал остановки
		for w := range m.workers {
			if i >= currentWorkersCount-maxWorkerCount {
				break
			}
			select {
			case w.stopSignal <- signal:
			default:
				//Если данный исполнитель уже получил сигнал, то продолжим
			}
			i++
		}
	} else {
		m.vacancies = make(chan void, 1)
		m.vacancies <- signal
	}
	m.notifyForVacancies()
}

//notifyForVacancies уведомляет все ожидающие запросы
//об изменившихся вакансиях
func (m *Manager) notifyForVacancies() {
	for {
		select {
		case m.vacancyChanged <- signal:
			break
		default:
			return
		}
	}
}

//isInfiniteWorkersAllowed возвращает признак  отсутсвия ограничения
//количества одновременно работающих исполнителей
func (m *Manager) isInfiniteWorkersAllowed() bool {
	return m.maxWorkerCount == 0
}
