package otasker

import (
	"fmt"
	"sync"
	"time"
)

type void struct{}

var signal void

// Dispatcher позволяет распределять работу на несколько
// исполнителей
type Dispatcher struct {
	taskQueue         chan *work
	vacancyChanged    chan void
	vacancies         chan void
	maxProcCount      int
	processors        map[*clientReqProc]void
	vacanciesLock     sync.RWMutex
	processorLifespan time.Duration
	hr                *hr
}

//NewDispatcher возвращает новый инициализированный объект Dispatcher
func NewDispatcher(hr *hr, maxProcCount int, processorLifespan time.Duration) (*Dispatcher, error) {
	if maxProcCount < 0 {
		return nil, fmt.Errorf("Negative value %v for maxProcCount is not allowed", maxProcCount)
	}
	if processorLifespan <= 0 {
		return nil, fmt.Errorf("Nonpositive value %v for processorLifespan is not allowed", processorLifespan)
	}
	dispatcher := &Dispatcher{
		taskQueue:         make(chan *work),
		vacancyChanged:    make(chan void),
		processors:        make(map[*clientReqProc]void),
		processorLifespan: processorLifespan,
		hr:                hr,
	}
	dispatcher.setupVacancies(maxProcCount, true)
	return dispatcher, nil
}

//AssignTask распределяет работу между несколькими исполнителями
//Возвращает флаг таймаута поиска исполнителя и ошибку
func (d *Dispatcher) AssignTask(request *work, maxProcCount int, timeout time.Duration) (bool, error) {
	if maxProcCount < 0 {
		return false, fmt.Errorf("Negative value %v for maxProcCount is not allowed", maxProcCount)
	}
	d.setupVacancies(maxProcCount, false)

	vacancies := d.vacancies
	timeoutTimer := time.NewTimer(timeout)
	defer timeoutTimer.Stop()
	for {
		//Первым делом проверим канал свободных исполнителей
		select {
		case d.taskQueue <- request:
			{
				//Задача назначена свободному исполнителю
				//Обновим вакансии и завершим работу менеджера
				//по обработке запроса
				return false, nil
			}
		default:
		}
		//Свободных не нашлось. Будем ждать
		select {
		case d.taskQueue <- request:
			{
				//Задача назначена свободному исполнителю
				//Обновим вакансии и завершим работу менеджера
				//по обработке запроса
				return false, nil
			}
		case <-d.vacancyChanged:
			{
				//Обновим список вакансий
				vacancies = d.vacancies
			}
		case <-vacancies:
			{
				//Свободных исполнителей нет, но есть вакансии
				//создадим нового исполнителя
				d.createProcessor()
			}
		case <-timeoutTimer.C:
			{
				//Исполнитель не был назначен в течение отведённого времени
				return true, nil
			}
		}
	}
}

//BreakAll останавливает всех исполнителей
func (d *Dispatcher) BreakAll() error {
	d.vacanciesLock.Lock()
	defer d.vacanciesLock.Unlock()
	var errors error
	for proc := range d.processors {
		select {
		case proc.stopSignal <- signal:
			{
			}
		default:
			{
			}
		}
		err := proc.Break()
		if err != nil {
			errors = fmt.Errorf("%s\n Error occurred %s", errors, err)
		}
	}
	return errors
}

//createProcessor создаёт нового исполнителя и регистрирует
//его в справочнике исполнителей workers
func (d *Dispatcher) createProcessor() {
	procStopped := func(p *clientReqProc) {
		d.vacanciesLock.Lock()
		delete(d.processors, p)
		if d.maxProcCount > len(d.processors) {
			d.vacancies <- signal
		}
		if d.hr != nil {
			d.hr.ProcessorStopped(p)
			if len(d.processors) == 0 {
				d.hr.AllProcessorsStopped(d)
			}
		}
		d.vacanciesLock.Unlock()
	}
	d.vacanciesLock.Lock()
	defer d.vacanciesLock.Unlock()
	processor := &clientReqProc{
		stopSignal:   make(chan void, 1),
		stopCallback: procStopped,
	}
	d.processors[processor] = signal
	if d.isInfiniteProcsAllowed() {
		d.vacancies <- signal
	}
	go processor.listen(d.taskQueue, d.processorLifespan)
	if d.hr != nil {
		d.hr.ProcessorCreated(processor)
	}
}

//setupVacancies управляет количеством текущих вакансий
func (d *Dispatcher) setupVacancies(maxProcCount int, isInit bool) {
	d.vacanciesLock.Lock()
	defer d.vacanciesLock.Unlock()
	if !isInit && d.maxProcCount == maxProcCount {
		return
	}
	d.maxProcCount = maxProcCount

	if !d.isInfiniteProcsAllowed() {
		currentProcsCount := len(d.processors)
		d.vacancies = make(chan void, maxProcCount)
		//Создадим в новом канале вакансий вакансии в количестве maxProcCount - currentProcsCount
		for i := currentProcsCount; i < maxProcCount; i++ {
			d.vacancies <- signal
		}
		i := 0
		//Пошлём "лишним" исполнителям сигнал остановки
		for w := range d.processors {
			if i >= currentProcsCount-maxProcCount {
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
		d.vacancies = make(chan void, 1)
		d.vacancies <- signal
	}
	d.notifyForVacancies()
}

//notifyForVacancies уведомляет все ожидающие запросы
//об изменившихся вакансиях
func (d *Dispatcher) notifyForVacancies() {
	for {
		select {
		case d.vacancyChanged <- signal:
			break
		default:
			return
		}
	}
}

//isInfiniteProcsAllowed возвращает признак  отсутсвия ограничения
//количества одновременно работающих исполнителей
func (d *Dispatcher) isInfiniteProcsAllowed() bool {
	return d.maxProcCount == 0
}
