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
package govern_test

// initialize
import (
	_ "github.com/apache/servicecomb-service-center/test"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"context"
	"testing"

	"github.com/onsi/ginkgo/reporters"

	"github.com/apache/servicecomb-service-center/pkg/proto"
	"github.com/apache/servicecomb-service-center/pkg/util"
	"github.com/apache/servicecomb-service-center/server/rest/govern"
)

func TestGovern(t *testing.T) {
	RegisterFailHandler(Fail)
	junitReporter := reporters.NewJUnitReporter("model.junit.xml")
	RunSpecsWithDefaultAndCustomReporters(t, "model Suite", []Reporter{junitReporter})
}

var governService proto.GovernServiceCtrlServer

var _ = BeforeSuite(func() {
	governService = govern.ServiceAPI
})

func getContext() context.Context {
	return util.WithNoCache(util.SetDomainProject(context.Background(), "default", "default"))
}
