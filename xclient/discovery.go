package xclient

import (
	"fmt"
	"math"
	"math/rand"
	"sync"
	"time"
)

type SelectMode int

const (
	RandomSelect     SelectMode = iota //select randomly
	RoundRobinSelect                   //select using Robbin algorithm
)

// 服务发现模块
type Discovery interface {
	Refresh() error //refresh from remote registry
	Update(servers []string) error
	Get(mode SelectMode) (string, error)
	GetAll() ([]string, error)
}

// 无需注册中心，服务列表手工维护的服务发现结构体
type MultiServerDiscovery struct {
	r       *rand.Rand
	mu      sync.RWMutex
	servers []string
	index   int
}

// new a multiserverDiscovery instance
func NewMultiServerDiscovery(servers []string) *MultiServerDiscovery {
	d := &MultiServerDiscovery{
		servers: servers,
		r:       rand.New(rand.NewSource(time.Now().UnixNano())),
	}

	d.index = d.r.Intn(math.MaxInt32 - 1)
	return d
}

var _Discovery = (*MultiServerDiscovery)(nil)

// Refresh doesn't make sense for MultiServersDiscovery, so ignore it
func (d *MultiServerDiscovery) Refresh() error {
	return nil
}

// Update the servers of discovery dynamically if needed
func (d *MultiServerDiscovery) Update(servers []string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.servers = servers
	return nil
}

// Get a server according to select mode such RandomSelect,RoundRobinSelect
func (d *MultiServerDiscovery) Get(mode SelectMode) (string, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	n := len(d.servers)
	if n == 0 {
		return "", fmt.Errorf("rpc discovery:no avaliable servers")
	}

	switch mode {
	case RandomSelect:
		//随机选择
		return d.servers[d.r.Intn(n)], nil
	case RoundRobinSelect:
		//轮询算法（非加权轮询）
		s := d.servers[d.index%n]
		d.index = (d.index + 1) % n
		return s, nil
	default:
		return "", fmt.Errorf("rpc discovery: not supported select mode")
	}
}

// return all the discovery's servers
func (d *MultiServerDiscovery) GetAll() ([]string, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	//return a copy of d.servers
	servers := make([]string, len(d.servers))
	copy(servers, d.servers)
	return servers, nil
}
