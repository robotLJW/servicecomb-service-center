/*
 * Licensed to the Apache Software Foundation (ASF) under one or more
 * contributor license agreements.  See the NOTICE file distributed with
 * this work for additional information regarding copyright ownership.
 * The ASF licenses this file to You under the Apache License, Version 2.0
 * (the "License"); you may not use this file except in compliance with
 * the License.  You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package util

import (
	"strings"

	pb "github.com/go-chassis/cari/discovery"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/x/bsonx"
)

type InstanceSlice []*pb.MicroServiceInstance

func (s InstanceSlice) Len() int {
	return len(s)
}

func (s InstanceSlice) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}

func (s InstanceSlice) Less(i, j int) bool {
	return s[i].InstanceId < s[j].InstanceId
}

func BuildIndexDoc(keys ...string) mongo.IndexModel {
	keysDoc := bsonx.Doc{}
	for _, key := range keys {
		keysDoc = keysDoc.Append(key, bsonx.Int32(1))
	}
	index := mongo.IndexModel{
		Keys: keysDoc,
	}
	return index
}

func StringBuilder(data []string) string {
	var str strings.Builder
	for index, value := range data {
		if index == 0 {
			str.WriteString(value)
		} else {
			str.WriteString("." + value)
		}
	}
	return str.String()
}
