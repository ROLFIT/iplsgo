// users
package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"sync"
)

type userInfo struct {
	IsSpecial                bool
	GrpID                    int32
	MaxConcurrentConnections int
}

func getUserInfo(name string) (userInfo, bool) {
	ulock.RLock()
	defer ulock.RUnlock()
	u, ok := ulist[strings.ToUpper(name)]
	if !ok {
		return emptyUserInfo, false
	}
	return *u, true
}

var emptyUserInfo = userInfo{IsSpecial: false, GrpID: -1, MaxConcurrentConnections: 1}

func getConnectionParams(user string, grps map[int32]string) (userInfo, string) {
	if user == "" {
		return emptyUserInfo, ""
	}
	uInfo, ok := getUserInfo(user)
	if !ok {
		return emptyUserInfo, ""
	}
	// Получаем глобальную строку соединения для ВСЕХ пользователей
	sid := *conectionString
	// если она пустая, ищем среди данных конфигурации
	if sid == "" {
		if sid, ok = grps[uInfo.GrpID]; !ok {
			return emptyUserInfo, ""
		}
	} else {
		if _, ok = grps[uInfo.GrpID]; !ok {
			return emptyUserInfo, ""
		}
	}
	return uInfo, sid
}

func getUsers() ([]byte, error) {
	ulock.RLock()
	defer ulock.RUnlock()
	return json.Marshal(ulist)
}

func updateUsers(users []byte) {
	ulock.RLock()
	needToParse := !bytes.Equal(prev, users)
	ulock.RUnlock()

	if needToParse {
		func() {
			ulock.Lock()
			defer ulock.Unlock()

			// TODO: copy так не работает -- надо сначала выделить память
			// в prev
			copy(prev, users)

			for k := range ulist {
				usersFree.Put(ulist[k])
				delete(ulist, k)
			}

			if len(users) == 0 {
				return
			}

			type _tUser struct {
				Name                     string
				IsSpecial                bool
				GRP_ID                   int32
				MaxConcurrentConnections int
			}
			var t = []_tUser{}
			if err := json.Unmarshal(users, &t); err != nil {
				logError(err)
			}

			for k := range t {
				u, ok := usersFree.Get().(*userInfo)
				if !ok {
					u = &userInfo{
						IsSpecial:                t[k].IsSpecial,
						GrpID:                    t[k].GRP_ID,
						MaxConcurrentConnections: t[k].MaxConcurrentConnections,
					}
				} else {
					u.IsSpecial = t[k].IsSpecial
					u.GrpID = t[k].GRP_ID
					u.MaxConcurrentConnections = t[k].MaxConcurrentConnections
				}
				ulist[strings.ToUpper(t[k].Name)] = u
			}
		}()
	}
}

var (
	ulock     sync.RWMutex
	ulist     = make(map[string]*userInfo)
	usersFree = sync.Pool{
		New: func() interface{} {
			return new(userInfo)
		},
	}
	prev []byte
)
