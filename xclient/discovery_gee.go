package xclient

import (
	"log"
	"net/http"
	"strings"
	"time"
)

type GeeRegistryDiscovery struct {
	*MultiServerDiscovery //嵌套服务发现模块
	registry              string
	timeout               time.Duration
	lastUpdate            time.Time
}

const defaultUpdateTimeout = time.Second * 10

func NewGeeRegistryDiscovery(regAddr string, timeout time.Duration) *GeeRegistryDiscovery {
	if timeout == 0 {
		timeout = defaultUpdateTimeout
	}

	d := &GeeRegistryDiscovery{
		MultiServerDiscovery: NewMultiServerDiscovery(make([]string, 0)),
		registry:             regAddr,
		timeout:              timeout,
	}

	return d
}

func (d *GeeRegistryDiscovery) Update(servers []string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.servers = servers
	d.lastUpdate = time.Now()
	return nil
}

func (d *GeeRegistryDiscovery) Refresh() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	//未超时则不用刷新服务列表
	if d.lastUpdate.Add(d.timeout).After(time.Now()) {
		return nil
	}

	log.Println("rpc registry:refresh servers from registry", d.registry)
	resp, err := http.Get(d.registry)
	if err != nil {
		log.Println("rpc registry refresh err:", err)
		return err
	}
	servers := strings.Split(resp.Header.Get("X-Geerpc-Servers"), ",")
	d.servers = make([]string, 0, len(servers))
	for _, server := range servers {
		if strings.TrimSpace(server) != "" {
			d.servers = append(d.servers, strings.TrimSpace(server))
		}
	}
	d.lastUpdate = time.Now()
	return nil

}

// 先调用refresh来确保服务列表未过期再调用父级的get
func (d *GeeRegistryDiscovery) Get(mode SelectMode) (string, error) {
	if err := d.Refresh(); err != nil {
		return "", err
	}

	return d.MultiServerDiscovery.Get(mode)
}

// 先调用refresh来确保服务列表未过期再调用父级的getAll
func (d *GeeRegistryDiscovery) GetAll() ([]string, error) {
	if err := d.Refresh(); err != nil {
		return nil, err
	}

	return d.MultiServerDiscovery.GetAll()
}
