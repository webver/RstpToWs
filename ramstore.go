package videoserver

import (
	"container/list"
	"fmt"
	"github.com/LdDl/vdk/av"
	"github.com/pkg/errors"
	"sync"
	"time"
)

type RamStore struct {
	startTime time.Time //Время 1ого полученного пакета
	pktTime   time.Duration

	firstPktId uint
	lastPktId  uint

	m            sync.Mutex
	storeSize    int
	maxStoreSize int

	keysQueue *list.List         //Храним последовательность ключей
	dataMap   map[uint]av.Packet //Храним сами пакеты
}

func NewRamStore(ramSize int) *RamStore {
	var ramStore RamStore

	ramStore.firstPktId = 0
	ramStore.lastPktId = 0

	ramStore.keysQueue = list.New()
	ramStore.dataMap = make(map[uint]av.Packet)

	ramStore.storeSize = 0
	ramStore.maxStoreSize = ramSize
	return &ramStore
}

func (ramStore *RamStore) AppendPkt(pkt av.Packet) error {
	var err error
	ramStore.m.Lock()
	defer ramStore.m.Unlock()

	ramStore.lastPktId++

	ramStore.keysQueue.PushBack(ramStore.lastPktId)
	ramStore.dataMap[ramStore.lastPktId] = pkt

	if len(ramStore.dataMap) == 0 {
		ramStore.startTime = time.Now()
		ramStore.pktTime = pkt.Time
	}

	ramStore.storeSize += len(pkt.Data)
	if ramStore.storeSize > ramStore.maxStoreSize && ramStore.keysQueue.Len() > 0 {
		elem := ramStore.keysQueue.Front()
		if key, ok := elem.Value.(uint); ok {
			pkt = ramStore.dataMap[key]
			ramStore.storeSize -= len(pkt.Data)
			delete(ramStore.dataMap, key)
			ramStore.firstPktId++
		} else {
			err = errors.New(fmt.Sprintf("Очередь содержит элемент неверного типа"))
		}
		ramStore.keysQueue.Remove(elem)
	}

	return err
}

func (ramStore *RamStore) FindPacket(time time.Time) (*av.Packet, error) {

	idx, err := ramStore.convertTimeToIndex(time)
	if err != nil {
		return nil, err
	}

	ramStore.m.Lock()
	defer ramStore.m.Unlock()

	if pkt, ok := ramStore.dataMap[idx]; ok {
		return &pkt, nil
	} else {
		return nil, errors.New(fmt.Sprintf("Не найден пакет с временем %v", time))
	}

}

func (ramStore *RamStore) convertTimeToIndex(time time.Time) (uint, error) {
	var key uint
	if time.After(ramStore.startTime) {
		key = uint((time.Sub(ramStore.startTime)).Microseconds() / ramStore.pktTime.Microseconds())
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
	//TODO clear queue and map
	return err
}
