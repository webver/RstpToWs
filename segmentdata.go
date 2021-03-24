package videoserver

import (
	"container/list"
	"fmt"
	"github.com/pkg/errors"
	"log"
	"sync"
	"time"
)

type SegmentStore struct {
	m sync.Mutex

	fistTime time.Time
	lastTime time.Time

	segmentCount int
	maxSegmentCount int
	segmentKeysQueue *list.List
	segmentDataMap map[string]SegmentData
}

type SegmentData struct {
	startTime time.Time
	endTime time.Time
}


func NewSegmentStore(maxSegmentCount int) *SegmentStore {
	var store SegmentStore

	store.segmentCount = 0
	store.maxSegmentCount = maxSegmentCount
	store.segmentKeysQueue = list.New()
	store.segmentDataMap = make(map[string]SegmentData)
	return &store
}

func (store *SegmentStore) AppendSegment(filename string, data SegmentData) error {
	var err error
	store.m.Lock()
	defer store.m.Unlock()

	if store.segmentCount == 0 {
		store.fistTime = data.startTime
	}

	store.lastTime = data.endTime

	store.segmentKeysQueue.PushBack(filename)
	store.segmentDataMap[filename] = data

	store.segmentCount ++
	if store.segmentCount > store.maxSegmentCount && store.segmentKeysQueue.Len() > 1 {
		elem := store.segmentKeysQueue.Front()
		if key, ok := elem.Value.(string); ok {
			store.segmentCount --
			delete(store.segmentDataMap, key)

			nextElem := elem.Next()
			if nextFileName, ok := nextElem.Value.(string); ok {
				store.fistTime = store.segmentDataMap[nextFileName].startTime
			} else {
				err = errors.New(fmt.Sprintf("Очередь содержит элемент неверного типа"))
				store.fistTime = store.lastTime
			}
		} else {
			err = errors.New(fmt.Sprintf("Очередь содержит элемент неверного типа"))
		}
		store.segmentKeysQueue.Remove(elem)
	}
	return err
}

func (store *SegmentStore) FindSegment(tm time.Time) (string, time.Time, error) {

	log.Printf("Поиск сегмента для времени %v", tm)
	var segmentStartTime time.Time

	store.m.Lock()
	defer store.m.Unlock()

	if tm.Before(store.lastTime) && tm.After(store.fistTime) {
		elem := store.segmentKeysQueue.Front()
		if elem == nil {
			return "", segmentStartTime, errors.New(fmt.Sprintf("Не найден сегмент с временем %v (последний %v  первый %v) ", tm, store.lastTime, store.fistTime))
		}
		for {
			//TODO дерево для бинарного поиска
			if segmentName, ok := elem.Value.(string); ok {
				if store.segmentDataMap[segmentName].startTime.Before(tm) && store.segmentDataMap[segmentName].endTime.After(tm) {
					segmentStartTime = store.segmentDataMap[segmentName].startTime
					return segmentName, segmentStartTime, nil
				}
			} else {
				return "", segmentStartTime, errors.New(fmt.Sprintf("Очередь содержит элемент неверного типа %v", elem.Value))
			}
			elem = elem.Next()
			if elem == nil {
				return "", segmentStartTime, errors.New(fmt.Sprintf("Не найден сегмент с временем %v (последний %v  первый %v) ", tm, store.lastTime, store.fistTime))
			}
		}

	} else {
		return "", segmentStartTime, errors.New(fmt.Sprintf("Не найден пакет с временем %v (последний %v  первый %v) ", tm, store.lastTime, store.fistTime))
	}
}