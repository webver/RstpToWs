package videoserver

import (
	"container/list"
	"fmt"
	"github.com/pkg/errors"
	"github.com/webver/vdk/av"
	"log"
	"sync"
	"time"
)

type QueueElementValue struct {
	PktIndex   uint
	IsKeyFrame uint
}

type MapElem struct {
	Pkt          av.Packet     `json:"pkt"`
	QueueElement *list.Element `json:"queueElement"`
}

type RamStore struct {
	startTime time.Time //Время 1ого полученного пакета
	pktTime   time.Duration

	rxPktNumber  uint
	firstPktTime time.Time
	lastPktTime  time.Time

	m            sync.Mutex
	storeSize    int
	maxStoreSize int

	keysQueue *list.List            //Храним последовательность ключей
	dataMap   map[time.Time]MapElem //Храним сами пакеты

}

func NewRamStore(ramSize int) *RamStore {
	var ramStore RamStore

	ramStore.rxPktNumber = 0
	//ramStore.firstPktTime = 0
	//ramStore.lastPktTime = 0

	ramStore.keysQueue = list.New()
	ramStore.dataMap = make(map[time.Time]MapElem)

	ramStore.storeSize = 0
	ramStore.maxStoreSize = ramSize
	return &ramStore
}

func (ramStore *RamStore) AppendPkt(pkt av.Packet) error {
	var err error
	ramStore.m.Lock()
	defer ramStore.m.Unlock()

	if ramStore.rxPktNumber == 0 {
		now := time.Now().UTC()
		ramStore.startTime = now.Add(-pkt.Time).Add(-pkt.CompositionTime)
		ramStore.firstPktTime = now
	}

	if ramStore.rxPktNumber == 1 {
		ramStore.pktTime = pkt.Time
	}

	ramStore.rxPktNumber++

	tm := ramStore.startTime.Add(pkt.Time).Add(pkt.CompositionTime)
	//tm, err := ramStore.convertTimeToIndex(ramStore.startTime.Add(pkt.Time).Add(pkt.CompositionTime))
	//if err != nil {
	//	return err
	//}

	ramStore.lastPktTime = tm

	if _, ok := ramStore.dataMap[tm]; ok {
		return nil
	}

	newElem := ramStore.keysQueue.PushBack(tm)
	ramStore.dataMap[tm] = MapElem{Pkt: pkt, QueueElement: newElem}

	ramStore.storeSize += len(pkt.Data)
	if ramStore.storeSize > ramStore.maxStoreSize && ramStore.keysQueue.Len() > 1 {
		elem := ramStore.keysQueue.Front()
		if key, ok := elem.Value.(time.Time); ok {
			if mapElem, ok := ramStore.dataMap[key]; ok {
				oldPkt := mapElem.Pkt
				ramStore.storeSize -= len(oldPkt.Data)
			} else {
				err = errors.New(fmt.Sprintf("Map не содержит удаляемый элемент"))
			}
			delete(ramStore.dataMap, key)

			nextElem := elem.Next()
			if nextElem != nil {
				if nextId, ok := nextElem.Value.(time.Time); ok {
					ramStore.firstPktTime = nextId
				} else {
					err = errors.New(fmt.Sprintf("Очередь не содержит следуюий"))
					ramStore.firstPktTime = ramStore.lastPktTime
				}
			} else {
				err = errors.New(fmt.Sprintf("Очередь не содержит следуюий"))
				ramStore.firstPktTime = ramStore.lastPktTime
			}
		} else {
			err = errors.New(fmt.Sprintf("Очередь содержит элемент неверного типа"))
		}
		ramStore.keysQueue.Remove(elem)
	}

	return err
}

func (ramStore *RamStore) FindPacket(timestamp time.Time) ([]av.Packet, error) {

	log.Printf("Получение скриншота за время %v", timestamp)
	//idx, err := ramStore.convertTimeToIndex(time)
	//if err != nil {
	//	return nil, err
	//}
	idx := timestamp.UTC()

	ramStore.m.Lock()
	defer ramStore.m.Unlock()

	if idx.Before(ramStore.lastPktTime) && idx.After(ramStore.firstPktTime) {
		elem := ramStore.keysQueue.Front()
		if elem == nil {
			return nil, errors.New(fmt.Sprintf("Не найден пакет с временем %v (индекс %v последний %v  первый %v) ", timestamp, idx, ramStore.lastPktTime, ramStore.firstPktTime))
		}
		for {
			if elem.Value.(time.Time).After(idx) {
				break
			}
			elem = elem.Next()
			if elem == nil {
				return nil, errors.New(fmt.Sprintf("Не найден пакет с временем %v (индекс %v последний %v  первый %v) ", timestamp, idx, ramStore.lastPktTime, ramStore.firstPktTime))
			}
		}

		indexArray := make([]time.Time, 0)
		for {
			indexArray = append(indexArray, elem.Value.(time.Time))

			if ramStore.dataMap[elem.Value.(time.Time)].Pkt.IsKeyFrame {
				break
			}
			elem = elem.Prev()
			if elem == nil {
				return nil, errors.New(fmt.Sprintf("Не найден ключевой фрейм для времени %v", timestamp))
			}
		}

		pktArray := make([]av.Packet, 0)
		for i := len(indexArray) - 1; i >= 0; i-- {
			pktArray = append(pktArray, ramStore.dataMap[indexArray[i]].Pkt)
		}

		return pktArray, nil
	} else {
		return nil, errors.New(fmt.Sprintf("Не найден пакет с временем %v (индекс %v последний %v  первый %v) ", timestamp, idx, ramStore.lastPktTime, ramStore.firstPktTime))
	}

}

func (ramStore *RamStore) convertTimeToIndex(time time.Time) (uint, error) {
	var key uint

	if ramStore != nil && ramStore.pktTime != 0 && time.After(ramStore.startTime) {
		key = uint((time.Sub(ramStore.startTime)) / ramStore.pktTime)
	} else {
		return 0, errors.New("Заданное время не попадает в диапазон")
	}
	return key, nil
}

func (ramStore *RamStore) Clear() error {
	var err error
	ramStore.m.Lock()
	defer ramStore.m.Unlock()

	ramStore.rxPktNumber = 0
	//ramStore.firstPktTime = 0
	//ramStore.lastPktTime = 0

	for k := range ramStore.dataMap {
		delete(ramStore.dataMap, k)
	}

	ramStore.keysQueue.Init()
	ramStore.storeSize = 0

	return err
}
