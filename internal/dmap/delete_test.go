// Copyright 2018-2020 Burak Sezer
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package dmap

import (
	"bytes"
	"github.com/buraksezer/olric/internal/cluster/partitions"
	"github.com/buraksezer/olric/internal/discovery"
	"github.com/buraksezer/olric/internal/protocol"
	"testing"
	"time"

	"github.com/buraksezer/olric/internal/testcluster"
	"github.com/buraksezer/olric/internal/testutil"
)

func Test_Delete_Cluster(t *testing.T) {
	cluster := testcluster.New(NewService)
	s1 := cluster.AddMember(nil).(*Service)
	s2 := cluster.AddMember(nil).(*Service)
	defer cluster.Shutdown()

	dm1, err := s1.NewDMap("mymap")
	if err != nil {
		t.Fatalf("Expected nil. Got: %v", err)
	}
	for i := 0; i < 10; i++ {
		err = dm1.Put(testutil.ToKey(i), testutil.ToVal(i))
		if err != nil {
			t.Fatalf("Expected nil. Got: %v", err)
		}
	}

	dm2, err := s2.NewDMap("mymap")
	if err != nil {
		t.Fatalf("Expected nil. Got: %v", err)
	}

	for i := 0; i < 10; i++ {
		err = dm2.Delete(testutil.ToKey(i))
		if err != nil {
			t.Fatalf("Expected nil. Got: %v", err)
		}

		_, err = dm2.Get(testutil.ToKey(i))
		if err != ErrKeyNotFound {
			t.Fatalf("Expected ErrKeyNotFound. Got: %v", err)
		}
	}
}

func Test_Delete_Lookup(t *testing.T) {
	cluster := testcluster.New(NewService)
	s1 := cluster.AddMember(nil).(*Service)
	cluster.AddMember(nil)
	defer cluster.Shutdown()

	dm1, err := s1.NewDMap("mymap")
	if err != nil {
		t.Fatalf("Expected nil. Got: %v", err)
	}
	for i := 0; i < 10; i++ {
		err = dm1.Put(testutil.ToKey(i), testutil.ToVal(i))
		if err != nil {
			t.Fatalf("Expected nil. Got: %v", err)
		}
	}

	s3 := cluster.AddMember(nil).(*Service)

	dm2, err := s3.NewDMap("mymap")
	if err != nil {
		t.Fatalf("Expected nil. Got: %v", err)
	}

	for i := 0; i < 10; i++ {
		err = dm2.Delete(testutil.ToKey(i))
		if err != nil {
			t.Fatalf("Expected nil. Got: %v", err)
		}

		_, err = dm2.Get(testutil.ToKey(i))
		if err != ErrKeyNotFound {
			t.Fatalf("Expected ErrKeyNotFound. Got: %v", err)
		}
	}
}

func Test_Delete_StaleFragments(t *testing.T) {
	cluster := testcluster.New(NewService)
	s1 := cluster.AddMember(nil).(*Service)
	s2 := cluster.AddMember(nil).(*Service)
	defer cluster.Shutdown()

	dm1, err := s1.NewDMap("mymap")
	if err != nil {
		t.Fatalf("Expected nil. Got: %v", err)
	}
	for i := 0; i < 100; i++ {
		err = dm1.Put(testutil.ToKey(i), testutil.ToVal(i))
		if err != nil {
			t.Fatalf("Expected nil. Got: %v", err)
		}
	}

	dm2, err := s2.NewDMap("mymap")
	if err != nil {
		t.Fatalf("Expected nil. Got: %v", err)
	}

	for i := 0; i < 100; i++ {
		err = dm2.Delete(testutil.ToKey(i))
		if err != nil {
			t.Fatalf("Expected nil. Got: %v", err)
		}

		_, err = dm2.Get(testutil.ToKey(i))
		if err != ErrKeyNotFound {
			t.Fatalf("Expected ErrKeyNotFound. Got: %v", err)
		}
	}

	s1.wg.Add(1)
	go s1.janitor()
	s2.wg.Add(1)
	go s2.janitor()

	var dc int32
	for i := 0; i < 1000; i++ {
		dc = 0
		for partID := uint64(0); partID < s1.config.PartitionCount; partID++ {
			for _, instance := range []*Service{s1, s2} {
				part := instance.primary.PartitionById(partID)
				part.Map().Range(func(name, dm interface{}) bool { dc++; return true })

				bpart := instance.backup.PartitionById(partID)
				bpart.Map().Range(func(name, dm interface{}) bool { dc++; return true })
			}
		}
		if dc == 0 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if dc != 0 {
		t.Fatalf("Expected dmap count is 0. Got: %d", dc)
	}
}

func Test_Delete_PreviousOwner(t *testing.T) {
	cluster := testcluster.New(NewService)
	s := cluster.AddMember(nil).(*Service)
	defer cluster.Shutdown()

	dm, err := s.NewDMap("mydmap")
	if err != nil {
		t.Fatalf("Expected nil. Got: %v", err)
	}
	err = dm.Put("mykey", "myvalue")
	if err != nil {
		t.Fatalf("Expected nil. Got: %v", err)
	}
	req := protocol.NewDMapMessage(protocol.OpDelete)
	req.SetBuffer(new(bytes.Buffer))
	req.SetDMap("mydmap")
	req.SetKey("mykey")
	resp := req.Response(nil)
	s.deletePrevOperation(resp, req)
	if resp.Status() != protocol.StatusOK {
		t.Fatalf("Expected StatusOK (%d). Got: %d", protocol.StatusOK, resp.Status())
	}

	_, err = dm.Get("mykey")
	if err != ErrKeyNotFound {
		t.Fatalf("Expected ErrKeyNotFound. Got: %v", err)
	}
}


func Test_Delete_DeleteKeyValFromPreviousOwners(t *testing.T) {
	cluster := testcluster.New(NewService)
	s := cluster.AddMember(nil).(*Service)
	cluster.AddMember(nil)
	defer cluster.Shutdown()

	dm, err := s.NewDMap("mydmap")
	if err != nil {
		t.Fatalf("Expected nil. Got: %v", err)
	}
	err = dm.Put("mykey", "myvalue")
	if err != nil {
		t.Fatalf("Expected nil. Got: %v", err)
	}

	// Prepare fragmented partition owners list
	hkey := partitions.HKey("mydmap", "mykey")
	owners := s.primary.PartitionOwnersByHKey(hkey)
	owner := owners[len(owners)-1]

	var data []discovery.Member
	for _, member := range s.rt.Discovery().GetMembers() {
		if member.CompareByID(owner) {
			continue
		}
		data = append(data, member)
	}
	// this has to be the last one
	data = append(data, owner)
	err = dm.deleteFromPreviousOwners("mykey", data)
	if err != nil {
		t.Fatalf("Expected nil. Got: %v", err)
	}
}