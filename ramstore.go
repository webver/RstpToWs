package videoserver

import (
	"container/list"
	"fmt"
	"github.com/LdDl/vdk/av"
	"github.com/pkg/errors"
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

	rxPktNumber uint
	firstPktId  uint
	lastPktId   uint

	m            sync.Mutex
	storeSize    int
	maxStoreSize int

	keysQueue *list.List       //Храним последовательность ключей
	dataMap   map[uint]MapElem //Храним сами пакеты
}

func NewRamStore(ramSize int) *RamStore {
	var ramStore RamStore

	ramStore.rxPktNumber = 0
	ramStore.firstPktId = 0
	ramStore.lastPktId = 0

	ramStore.keysQueue = list.New()
	ramStore.dataMap = make(map[uint]MapElem)

	ramStore.storeSize = 0
	ramStore.maxStoreSize = ramSize
	return &ramStore
}

func (ramStore *RamStore) AppendPkt(pkt av.Packet) error {
	var err error
	ramStore.m.Lock()
	defer ramStore.m.Unlock()

	if ramStore.rxPktNumber == 0 {
		ramStore.startTime = time.Now().Add(-pkt.Time)
	}

	if ramStore.rxPktNumber == 1 {
		ramStore.pktTime = pkt.Time
	}

	ramStore.rxPktNumber++

	idx, err := ramStore.convertTimeToIndex(ramStore.startTime.Add(pkt.Time),0)
	if err != nil {
		return err
	}

	for {
		if _, ok := ramStore.dataMap[idx]; ok {
			idx ++
		} else {
			break
		}
	}

	ramStore.lastPktId = idx

	newElem := ramStore.keysQueue.PushBack(idx)
	ramStore.dataMap[idx] = MapElem{Pkt: pkt, QueueElement: newElem}

	ramStore.storeSize += len(pkt.Data)
	if ramStore.storeSize > ramStore.maxStoreSize && ramStore.keysQueue.Len() > 0 {
		elem := ramStore.keysQueue.Front()
		if key, ok := elem.Value.(uint); ok {
			if mapElem, ok := ramStore.dataMap[key]; ok {
				oldPkt := mapElem.Pkt
				ramStore.storeSize -= len(oldPkt.Data)
			} else {
				err = errors.New(fmt.Sprintf("Map не содержит удаляемый элемент"))
			}
			delete(ramStore.dataMap, key)

			nextElem := elem.Next()
			if nextId, ok := nextElem.Value.(uint); ok {
				ramStore.firstPktId = nextId
			} else {
				err = errors.New(fmt.Sprintf("Очередь не содержит следуюий"))
				ramStore.firstPktId = ramStore.lastPktId
			}
		} else {
			err = errors.New(fmt.Sprintf("Очередь содержит элемент неверного типа"))
		}
		ramStore.keysQueue.Remove(elem)
	}

	return err
}

func (ramStore *RamStore) FindPacket(time time.Time) ([]MapElem, error) {

	log.Printf("Получение скриншота за время %v", time)
	idx, err := ramStore.convertTimeToIndex(time,0)
	if err != nil {
		return nil, err
	}

	ramStore.m.Lock()
	defer ramStore.m.Unlock()

	if mapElem, ok := ramStore.dataMap[idx]; ok {
		elem := mapElem.QueueElement.Prev()
		pktArray := make([]MapElem, 0)
		for {
			pktArray = append(pktArray, ramStore.dataMap[elem.Value.(uint)])
			if ramStore.dataMap[elem.Value.(uint)].Pkt.IsKeyFrame {
				break
			}
			elem = elem.Prev()
			if elem == nil {
				return nil, errors.New(fmt.Sprintf("Не найден ключевой фрейм для времени %v", time))
			}

		}
		return pktArray, nil
	} else {
		return nil, errors.New(fmt.Sprintf("Не найден пакет с временем %v (индекс %d последний %d  первый %d) ", time, idx, ramStore.lastPktId, ramStore.firstPktId))
	}

}

func (ramStore *RamStore) convertTimeToIndex(time time.Time, index uint) (uint, error) {
	var key uint


	if ramStore != nil && ramStore.pktTime != 0 && time.After(ramStore.startTime) {
		key = uint((time.Sub(ramStore.startTime)) / ramStore.pktTime)*1000 + (index%1000)
	} else {
		return 0, errors.New("Заданное время не попадает в диапазон")
	}
	return key, nil
}

func (ramStore *RamStore) Clear() error {
	var err error
	ramStore.m.Lock()
	defer ramStore.m.Unlock()

	ramStore.firstPktId = 0
	ramStore.lastPktId = 0

	for k := range ramStore.dataMap {
		delete(ramStore.dataMap, k)
	}

	elem := ramStore.keysQueue.Front()
	for {
		deletedElem := elem
		elem = elem.Next()
		if deletedElem != nil {
			ramStore.keysQueue.Remove(deletedElem)
		} else {
			break
		}
	}

	return err
}
