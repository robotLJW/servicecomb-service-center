/*
 * Licensed to the Apache Software Foundation (ASF) under one or more
 * contributor license agreements.  See the NOTICE file distributed with
 * this work for additional information regarding copyright ownership.
 * The ASF licenses this file to You under the Apache License, Version 2.0
 * (the "License"); you may not use this file except request compliance with
 * the License.  You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to request writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/go-chassis/cari/discovery"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/apache/servicecomb-service-center/datasource"
	"github.com/apache/servicecomb-service-center/datasource/mongo/client"
	"github.com/apache/servicecomb-service-center/datasource/mongo/model"
	mutil "github.com/apache/servicecomb-service-center/datasource/mongo/util"
	"github.com/apache/servicecomb-service-center/pkg/gopool"
	"github.com/apache/servicecomb-service-center/pkg/log"
	"github.com/apache/servicecomb-service-center/pkg/util"
	apt "github.com/apache/servicecomb-service-center/server/core"
	"github.com/apache/servicecomb-service-center/server/plugin/quota"
	"github.com/apache/servicecomb-service-center/server/plugin/uuid"
)

func (ds *DataSource) RegisterService(ctx context.Context, request *discovery.CreateServiceRequest) (*discovery.CreateServiceResponse, error) {
	service := request.Service
	remoteIP := util.GetIPFromContext(ctx)
	domain := util.ParseDomain(ctx)
	project := util.ParseProject(ctx)
	serviceFlag := util.StringJoin([]string{
		service.Environment, service.AppId, service.ServiceName, service.Version}, "/")
	//todo add quota check
	requestServiceID := service.ServiceId

	if len(requestServiceID) == 0 {
		ctx = util.SetContext(ctx, uuid.ContextKey, util.StringJoin([]string{domain, project, service.Environment, service.AppId, service.ServiceName, service.Alias, service.Version}, "/"))
		service.ServiceId = uuid.Generator().GetServiceID(ctx)
	}
	service.Timestamp = strconv.FormatInt(time.Now().Unix(), 10)
	service.ModTimestamp = service.Timestamp
	// the service unique index in table is (serviceId/serviceEnv,serviceAppid,servicename,serviceVersion)
	if len(service.Alias) != 0 {
		serviceID, err := mutil.GetServiceID(ctx, &discovery.MicroServiceKey{
			Environment: service.Environment,
			AppId:       service.AppId,
			ServiceName: service.ServiceName,
			Version:     service.Version,
			Alias:       service.Alias,
		})
		if err != nil && !errors.Is(err, datasource.ErrNoData) {
			return &discovery.CreateServiceResponse{
				Response: discovery.CreateResponse(discovery.ErrUnavailableBackend, err.Error()),
			}, err
		}
		if len(serviceID) != 0 {
			if len(requestServiceID) != 0 && requestServiceID != serviceID {
				log.Warn(fmt.Sprintf("create micro-service[%s] failed, service already exists, operator: %s",
					serviceFlag, remoteIP))
				return &discovery.CreateServiceResponse{
					Response: discovery.CreateResponse(discovery.ErrServiceAlreadyExists,
						"ServiceID conflict or found the same service with different id."),
				}, nil
			}
			return &discovery.CreateServiceResponse{
				Response:  discovery.CreateResponse(discovery.ResponseSuccess, "register service successfully"),
				ServiceId: serviceID,
			}, nil
		}
	}
	insertRes, err := client.GetMongoClient().Insert(ctx, model.CollectionService, &model.Service{Domain: domain, Project: project, Service: service})
	if err != nil {
		if client.IsDuplicateKey(err) {
			serviceIDInner, err := mutil.GetServiceID(ctx, &discovery.MicroServiceKey{
				Environment: service.Environment,
				AppId:       service.AppId,
				ServiceName: service.ServiceName,
				Version:     service.Version,
			})
			if err != nil && !errors.Is(err, datasource.ErrNoData) {
				return &discovery.CreateServiceResponse{
					Response: discovery.CreateResponse(discovery.ErrUnavailableBackend, err.Error()),
				}, err
			}
			// serviceid conflict with the service in the database
			if len(requestServiceID) != 0 && serviceIDInner != requestServiceID {
				return &discovery.CreateServiceResponse{
					Response: discovery.CreateResponse(discovery.ErrServiceAlreadyExists,
						"ServiceID conflict or found the same service with different id."),
				}, nil
			}
			return &discovery.CreateServiceResponse{
				Response:  discovery.CreateResponse(discovery.ResponseSuccess, "register service successfully"),
				ServiceId: serviceIDInner,
			}, nil
		}
		log.Error(fmt.Sprintf("create micro-service[%s] failed, service already exists, operator: %s",
			serviceFlag, remoteIP), err)
		return &discovery.CreateServiceResponse{
			Response: discovery.CreateResponse(discovery.ErrInternal, err.Error()),
		}, err
	}

	log.Info(fmt.Sprintf("create micro-service[%s][%s] successfully,operator: %s", service.ServiceId, insertRes.InsertedID, remoteIP))

	return &discovery.CreateServiceResponse{
		Response:  discovery.CreateResponse(discovery.ResponseSuccess, "Register service successfully"),
		ServiceId: service.ServiceId,
	}, nil
}

func (ds *DataSource) GetServices(ctx context.Context, request *discovery.GetServicesRequest) (*discovery.GetServicesResponse, error) {

	domain := util.ParseDomain(ctx)
	project := util.ParseProject(ctx)

	filter := bson.M{model.ColumnDomain: domain, model.ColumnProject: project}

	services, err := mutil.GetMicroServices(ctx, filter)
	if err != nil {
		return &discovery.GetServicesResponse{
			Response: discovery.CreateResponse(discovery.ErrInternal, "get services data failed."),
		}, nil
	}

	return &discovery.GetServicesResponse{
		Response: discovery.CreateResponse(discovery.ResponseSuccess, "Get all services successfully."),
		Services: services,
	}, nil
}

func (ds *DataSource) GetApplications(ctx context.Context, request *discovery.GetAppsRequest) (*discovery.GetAppsResponse, error) {
	domain := util.ParseDomain(ctx)
	project := util.ParseProject(ctx)

	filter := bson.M{
		model.ColumnDomain:  domain,
		model.ColumnProject: project,
		mutil.StringBuilder([]string{model.ColumnService, model.ColumnEnv}): request.Environment}

	services, err := mutil.GetMicroServices(ctx, filter)
	if err != nil {
		return &discovery.GetAppsResponse{
			Response: discovery.CreateResponse(discovery.ErrInternal, "get services data failed."),
		}, nil
	}
	l := len(services)
	if l == 0 {
		return &discovery.GetAppsResponse{
			Response: discovery.CreateResponse(discovery.ResponseSuccess, "Get all applications successfully."),
		}, nil
	}
	apps := make([]string, 0, l)
	hash := make(map[string]struct{}, l)
	for _, svc := range services {
		if !request.WithShared && apt.IsGlobal(discovery.MicroServiceToKey(util.ParseDomainProject(ctx), svc)) {
			continue
		}
		if _, ok := hash[svc.AppId]; ok {
			continue
		}
		hash[svc.AppId] = struct{}{}
		apps = append(apps, svc.AppId)
	}
	return &discovery.GetAppsResponse{
		Response: discovery.CreateResponse(discovery.ResponseSuccess, "Get all applications successfully."),
		AppIds:   apps,
	}, nil
}

func (ds *DataSource) GetService(ctx context.Context, request *discovery.GetServiceRequest) (*discovery.GetServiceResponse, error) {
	svc, err := mutil.GetServiceByID(ctx, request.ServiceId)
	if err != nil {
		if errors.Is(err, datasource.ErrNoData) {
			log.Debug(fmt.Sprintf("service %s not exist in model", request.ServiceId))
			return &discovery.GetServiceResponse{
				Response: discovery.CreateResponse(discovery.ErrServiceNotExists, "Service not exist."),
			}, nil
		}
		log.Error(fmt.Sprintf("failed to get single service %s from mongo", request.ServiceId), err)
		return &discovery.GetServiceResponse{
			Response: discovery.CreateResponse(discovery.ErrInternal, "get service data from mongodb failed."),
		}, err
	}
	return &discovery.GetServiceResponse{
		Response: discovery.CreateResponse(discovery.ResponseSuccess, "Get service successfully."),
		Service:  svc.Service,
	}, nil
}

func (ds *DataSource) ExistServiceByID(ctx context.Context, request *discovery.GetExistenceByIDRequest) (*discovery.GetExistenceByIDResponse, error) {
	exist, err := mutil.ServiceExistID(ctx, request.ServiceId)
	if err != nil {
		return &discovery.GetExistenceByIDResponse{
			Response: discovery.CreateResponse(discovery.ErrInternal, "Check service exist failed."),
			Exist:    false,
		}, err
	}

	return &discovery.GetExistenceByIDResponse{
		Response: discovery.CreateResponse(discovery.ResponseSuccess, "Check ExistService successfully."),
		Exist:    exist,
	}, nil
}

func (ds *DataSource) ExistService(ctx context.Context, request *discovery.GetExistenceRequest) (*discovery.GetExistenceResponse, error) {
	domainProject := util.ParseDomainProject(ctx)
	serviceFlag := util.StringJoin([]string{
		request.Environment, request.AppId, request.ServiceName, request.Version}, "/")

	ids, exist, err := mutil.FindServiceIds(ctx, request.Version, &discovery.MicroServiceKey{
		Environment: request.Environment,
		AppId:       request.AppId,
		ServiceName: request.ServiceName,
		Alias:       request.ServiceName,
		Version:     request.Version,
		Tenant:      domainProject,
	})
	if err != nil {
		log.Error(fmt.Sprintf("micro-service[%s] exist failed, find serviceIDs failed", serviceFlag), err)
		return &discovery.GetExistenceResponse{
			Response: discovery.CreateResponse(discovery.ErrInternal, err.Error()),
		}, err
	}
	if !exist {
		log.Info(fmt.Sprintf("micro-service[%s] exist failed, service does not exist", serviceFlag))
		return &discovery.GetExistenceResponse{
			Response: discovery.CreateResponse(discovery.ErrServiceNotExists, serviceFlag+" does not exist."),
		}, nil
	}
	if len(ids) == 0 {
		log.Info(fmt.Sprintf("micro-service[%s] exist failed, version mismatch", serviceFlag))
		return &discovery.GetExistenceResponse{
			Response: discovery.CreateResponse(discovery.ErrServiceVersionNotExists, serviceFlag+" version mismatch."),
		}, nil
	}
	return &discovery.GetExistenceResponse{
		Response:  discovery.CreateResponse(discovery.ResponseSuccess, "get service id successfully."),
		ServiceId: ids[0], // 约定多个时，取较新版本
	}, nil
}

func (ds *DataSource) UnregisterService(ctx context.Context, request *discovery.DeleteServiceRequest) (*discovery.DeleteServiceResponse, error) {
	res, err := ds.DelServicePri(ctx, request.ServiceId, request.Force)
	if err != nil {
		return &discovery.DeleteServiceResponse{
			Response: discovery.CreateResponse(discovery.ErrInternal, "Delete service failed"),
		}, err
	}
	return &discovery.DeleteServiceResponse{
		Response: res,
	}, nil
}

func (ds *DataSource) DelServicePri(ctx context.Context, serviceID string, force bool) (*discovery.Response, error) {
	remoteIP := util.GetIPFromContext(ctx)
	title := "delete"
	if force {
		title = "force delete"
	}

	if serviceID == apt.Service.ServiceId {
		log.Error(fmt.Sprintf("%s micro-service %s failed, operator: %s", title, serviceID, remoteIP), model.ErrNotAllowDeleteSC)
		return discovery.CreateResponse(discovery.ErrInvalidParams, model.ErrNotAllowDeleteSC.Error()), nil
	}
	microservice, err := mutil.GetService(ctx, mutil.GeneratorServiceFilter(ctx, serviceID))
	if err != nil {
		if errors.Is(err, datasource.ErrNoData) {
			log.Debug(fmt.Sprintf("%s micro-service %s failed, service does not exist, operator: %s",
				title, serviceID, remoteIP))
			return discovery.CreateResponse(discovery.ErrServiceNotExists, "Service does not exist."), nil
		}
		log.Error(fmt.Sprintf("%s micro-service %s failed, get service file failed, operator: %s",
			title, serviceID, remoteIP), err)
		return discovery.CreateResponse(discovery.ErrInternal, err.Error()), err
	}
	// 强制删除，则与该服务相关的信息删除，非强制删除： 如果作为该被依赖（作为provider，提供服务,且不是只存在自依赖）或者存在实例，则不能删除
	if !force {
		dr := mutil.NewProviderDependencyRelation(ctx, util.ParseDomainProject(ctx), microservice.Service)
		services, err := dr.GetDependencyConsumerIds()
		if err != nil {
			log.Error(fmt.Sprintf("delete micro-service[%s] failed, get service dependency failed, operator: %s",
				serviceID, remoteIP), err)
			return discovery.CreateResponse(discovery.ErrInternal, err.Error()), err
		}
		if l := len(services); l > 1 || (l == 1 && services[0] != serviceID) {
			log.Error(fmt.Sprintf("delete micro-service[%s] failed, other services[%d] depend on it, operator: %s",
				serviceID, l, remoteIP), err)
			return discovery.CreateResponse(discovery.ErrDependedOnConsumer, "Can not delete this service, other service rely it."), err
		}
		//todo wait for dep interface
		instancesExist, err := client.GetMongoClient().DocExist(ctx, model.CollectionInstance, bson.M{mutil.StringBuilder([]string{model.ColumnInstance, model.ColumnServiceID}): serviceID})
		if err != nil {
			log.Error(fmt.Sprintf("delete micro-service[%s] failed, get instances number failed, operator: %s",
				serviceID, remoteIP), err)
			return discovery.CreateResponse(discovery.ErrUnavailableBackend, err.Error()), err
		}
		if instancesExist {
			log.Error(fmt.Sprintf("delete micro-service[%s] failed, service deployed instances, operator: %s",
				serviceID, remoteIP), nil)
			return discovery.CreateResponse(discovery.ErrDeployedInstance, "Can not delete the service deployed instance(s)."), err
		}

	}

	schemaOps := client.MongoOperation{Table: model.CollectionSchema, Models: []mongo.WriteModel{mongo.NewDeleteManyModel().SetFilter(bson.M{model.ColumnServiceID: serviceID})}}
	rulesOps := client.MongoOperation{Table: model.CollectionRule, Models: []mongo.WriteModel{mongo.NewDeleteManyModel().SetFilter(bson.M{model.ColumnServiceID: serviceID})}}
	instanceOps := client.MongoOperation{Table: model.CollectionInstance, Models: []mongo.WriteModel{mongo.NewDeleteManyModel().SetFilter(bson.M{mutil.StringBuilder([]string{model.ColumnInstance, model.ColumnServiceID}): serviceID})}}
	serviceOps := client.MongoOperation{Table: model.CollectionService, Models: []mongo.WriteModel{mongo.NewDeleteOneModel().SetFilter(bson.M{mutil.StringBuilder([]string{model.ColumnService, model.ColumnServiceID}): serviceID})}}

	err = client.GetMongoClient().MultiTableBatchUpdate(ctx, []client.MongoOperation{schemaOps, rulesOps, instanceOps, serviceOps})
	if err != nil {
		log.Error(fmt.Sprintf("micro-service[%s] failed, operator: %s", serviceID, remoteIP), err)
		return discovery.CreateResponse(discovery.ErrUnavailableBackend, err.Error()), err
	}

	domainProject := util.ToDomainProject(microservice.Domain, microservice.Project)
	serviceKey := &discovery.MicroServiceKey{
		Tenant:      domainProject,
		Environment: microservice.Service.Environment,
		AppId:       microservice.Service.AppId,
		ServiceName: microservice.Service.ServiceName,
		Version:     microservice.Service.Version,
		Alias:       microservice.Service.Alias,
	}

	err = mutil.DeleteDependencyForDeleteService(domainProject, serviceID, serviceKey)
	if err != nil {
		log.Error(fmt.Sprintf("micro-service[%s] failed, operator: %s", serviceID, remoteIP), err)
		return discovery.CreateResponse(discovery.ErrUnavailableBackend, err.Error()), err
	}

	return discovery.CreateResponse(discovery.ResponseSuccess, "Unregister service successfully."), nil
}

func (ds *DataSource) UpdateService(ctx context.Context, request *discovery.UpdateServicePropsRequest) (*discovery.UpdateServicePropsResponse, error) {
	updateData := bson.M{
		"$set": bson.M{
			mutil.StringBuilder([]string{model.ColumnService, model.ColumnModTime}):  strconv.FormatInt(time.Now().Unix(), 10),
			mutil.StringBuilder([]string{model.ColumnService, model.ColumnProperty}): request.Properties}}
	err := mutil.UpdateService(ctx, mutil.GeneratorServiceFilter(ctx, request.ServiceId), updateData)
	if err != nil {
		log.Error(fmt.Sprintf("update service %s properties failed, update mongo failed", request.ServiceId), err)
		return &discovery.UpdateServicePropsResponse{
			Response: discovery.CreateResponse(discovery.ErrUnavailableBackend, "Update doc in mongo failed."),
		}, nil
	}
	return &discovery.UpdateServicePropsResponse{
		Response: discovery.CreateResponse(discovery.ResponseSuccess, "Update service successfully."),
	}, nil
}

func (ds *DataSource) GetDeleteServiceFunc(ctx context.Context, serviceID string, force bool, serviceRespChan chan<- *discovery.DelServicesRspInfo) func(context.Context) {
	return func(_ context.Context) {
		serviceRst := &discovery.DelServicesRspInfo{
			ServiceId:  serviceID,
			ErrMessage: "",
		}
		resp, err := ds.DelServicePri(ctx, serviceID, force)
		if err != nil {
			serviceRst.ErrMessage = err.Error()
		} else if resp.GetCode() != discovery.ResponseSuccess {
			serviceRst.ErrMessage = resp.GetMessage()
		}

		serviceRespChan <- serviceRst
	}
}

func (ds *DataSource) GetServiceDetail(ctx context.Context, request *discovery.GetServiceRequest) (*discovery.GetServiceDetailResponse, error) {
	mgSvc, err := mutil.GetService(ctx, mutil.GeneratorServiceFilter(ctx, request.ServiceId))
	if err != nil {
		if errors.Is(err, datasource.ErrNoData) {
			return &discovery.GetServiceDetailResponse{
				Response: discovery.CreateResponse(discovery.ErrServiceNotExists, "Service does not exist."),
			}, nil
		}
		return &discovery.GetServiceDetailResponse{
			Response: discovery.CreateResponse(discovery.ErrInternal, err.Error()),
		}, err
	}
	svc := mgSvc.Service
	key := &discovery.MicroServiceKey{
		Environment: svc.Environment,
		AppId:       svc.AppId,
		ServiceName: svc.ServiceName,
	}
	versions, err := mutil.GetServicesVersions(ctx, mutil.GeneratorServiceVersionsFilter(ctx, key))
	if err != nil {
		log.Error(fmt.Sprintf("get service %s %s %s all versions failed", svc.Environment, svc.AppId, svc.ServiceName), err)
		return &discovery.GetServiceDetailResponse{
			Response: discovery.CreateResponse(discovery.ErrInternal, err.Error()),
		}, err
	}
	options := []string{"tags", "rules", "instances", "schemas", "dependencies"}
	serviceInfo, err := mutil.GetServiceDetailUtil(ctx, mgSvc, false, options)
	if err != nil {
		return &discovery.GetServiceDetailResponse{
			Response: discovery.CreateResponse(discovery.ErrInternal, err.Error()),
		}, err
	}
	serviceInfo.MicroService = svc
	serviceInfo.MicroServiceVersions = versions
	return &discovery.GetServiceDetailResponse{
		Response: discovery.CreateResponse(discovery.ResponseSuccess, "Get service successfully"),
		Service:  serviceInfo,
	}, nil

}

func (ds *DataSource) GetServicesInfo(ctx context.Context, request *discovery.GetServicesInfoRequest) (*discovery.GetServicesInfoResponse, error) {
	optionMap := make(map[string]struct{}, len(request.Options))
	for _, opt := range request.Options {
		optionMap[opt] = struct{}{}
	}

	options := make([]string, 0, len(optionMap))
	if _, ok := optionMap["all"]; ok {
		optionMap["statistics"] = struct{}{}
		options = []string{"tags", "rules", "instances", "schemas", "dependencies"}
	} else {
		for opt := range optionMap {
			options = append(options, opt)
		}
	}
	var st *discovery.Statistics
	if _, ok := optionMap["statistics"]; ok {
		var err error
		st, err = mutil.Statistics(ctx, request.WithShared)
		if err != nil {
			return &discovery.GetServicesInfoResponse{
				Response: discovery.CreateResponse(discovery.ErrInternal, err.Error()),
			}, err
		}
		if len(optionMap) == 1 {
			return &discovery.GetServicesInfoResponse{
				Response:   discovery.CreateResponse(discovery.ResponseSuccess, "Statistics successfully."),
				Statistics: st,
			}, nil
		}
	}
	services, err := mutil.GetServices(ctx, bson.M{})
	if err != nil {
		log.Error("get all services by domain failed", err)
		return &discovery.GetServicesInfoResponse{
			Response: discovery.CreateResponse(discovery.ErrInternal, err.Error()),
		}, err
	}
	allServiceDetails := make([]*discovery.ServiceDetail, 0, len(services))
	domainProject := util.ParseDomainProject(ctx)
	for _, mgSvc := range services {
		if !request.WithShared && apt.IsGlobal(discovery.MicroServiceToKey(domainProject, mgSvc.Service)) {
			continue
		}
		if len(request.AppId) > 0 {
			if request.AppId != mgSvc.Service.AppId {
				continue
			}
			if len(request.ServiceName) > 0 && request.ServiceName != mgSvc.Service.ServiceName {
				continue
			}
		}

		serviceDetail, err := mutil.GetServiceDetailUtil(ctx, mgSvc, request.CountOnly, options)
		if err != nil {
			return &discovery.GetServicesInfoResponse{
				Response: discovery.CreateResponse(discovery.ErrInternal, err.Error()),
			}, err
		}
		serviceDetail.MicroService = mgSvc.Service
		allServiceDetails = append(allServiceDetails, serviceDetail)
	}

	return &discovery.GetServicesInfoResponse{
		Response:          discovery.CreateResponse(discovery.ResponseSuccess, "Get services info successfully."),
		AllServicesDetail: allServiceDetails,
		Statistics:        nil,
	}, nil
}

func (ds *DataSource) AddTags(ctx context.Context, request *discovery.AddServiceTagsRequest) (*discovery.AddServiceTagsResponse, error) {
	err := mutil.UpdateService(ctx, mutil.GeneratorServiceFilter(ctx, request.ServiceId), bson.M{"$set": bson.M{model.ColumnTag: request.Tags}})
	if err == nil {
		return &discovery.AddServiceTagsResponse{
			Response: discovery.CreateResponse(discovery.ResponseSuccess, "Add service tags successfully."),
		}, nil
	}
	log.Error(fmt.Sprintf("update service %s tags failed.", request.ServiceId), err)
	if err == client.ErrNoDocuments {
		return &discovery.AddServiceTagsResponse{
			Response: discovery.CreateResponse(discovery.ErrServiceNotExists, err.Error()),
		}, nil
	}
	return &discovery.AddServiceTagsResponse{
		Response: discovery.CreateResponse(discovery.ErrInternal, err.Error()),
	}, nil
}

func (ds *DataSource) GetTags(ctx context.Context, request *discovery.GetServiceTagsRequest) (*discovery.GetServiceTagsResponse, error) {
	svc, err := mutil.GetService(ctx, mutil.GeneratorServiceFilter(ctx, request.ServiceId))
	if err != nil {
		if errors.Is(err, datasource.ErrNoData) {
			log.Debug(fmt.Sprintf("service %s not exist in model", request.ServiceId))
			return &discovery.GetServiceTagsResponse{
				Response: discovery.CreateResponse(discovery.ErrServiceNotExists, "Service does not exist"),
			}, nil
		}
		log.Error(fmt.Sprintf("failed to get service %s tags", request.ServiceId), err)
		return &discovery.GetServiceTagsResponse{
			Response: discovery.CreateResponse(discovery.ErrInternal, err.Error()),
		}, nil
	}
	return &discovery.GetServiceTagsResponse{
		Response: discovery.CreateResponse(discovery.ResponseSuccess, "Get service tags successfully."),
		Tags:     svc.Tags,
	}, nil
}

func (ds *DataSource) UpdateTag(ctx context.Context, request *discovery.UpdateServiceTagRequest) (*discovery.UpdateServiceTagResponse, error) {
	svc, err := mutil.GetService(ctx, mutil.GeneratorServiceFilter(ctx, request.ServiceId))
	if err != nil {
		if errors.Is(err, datasource.ErrNoData) {
			log.Debug(fmt.Sprintf("service %s not exist in model", request.ServiceId))
			return &discovery.UpdateServiceTagResponse{
				Response: discovery.CreateResponse(discovery.ErrServiceNotExists, "Service does not exist"),
			}, nil
		}
		log.Error(fmt.Sprintf("failed to get %s tags", request.ServiceId), err)
		return &discovery.UpdateServiceTagResponse{
			Response: discovery.CreateResponse(discovery.ErrInternal, err.Error()),
		}, nil
	}
	dataTags := svc.Tags
	if len(dataTags) > 0 {
		if _, ok := dataTags[request.Key]; !ok {
			return &discovery.UpdateServiceTagResponse{
				Response: discovery.CreateResponse(discovery.ErrTagNotExists, "Tag does not exist"),
			}, nil
		}
	}
	newTags := make(map[string]string, len(dataTags))
	for k, v := range dataTags {
		newTags[k] = v
	}
	newTags[request.Key] = request.Value

	err = mutil.UpdateService(ctx, mutil.GeneratorServiceFilter(ctx, request.ServiceId), bson.M{"$set": bson.M{model.ColumnTag: newTags}})
	if err != nil {
		log.Error(fmt.Sprintf("update service %s tags failed", request.ServiceId), err)
		return &discovery.UpdateServiceTagResponse{
			Response: discovery.CreateResponse(discovery.ErrInternal, err.Error()),
		}, nil
	}
	return &discovery.UpdateServiceTagResponse{
		Response: discovery.CreateResponse(discovery.ResponseSuccess, "Update service tag success."),
	}, nil
}

func (ds *DataSource) DeleteTags(ctx context.Context, request *discovery.DeleteServiceTagsRequest) (*discovery.DeleteServiceTagsResponse, error) {
	svc, err := mutil.GetService(ctx, mutil.GeneratorServiceFilter(ctx, request.ServiceId))
	if err != nil {
		if errors.Is(err, datasource.ErrNoData) {
			log.Debug(fmt.Sprintf("service %s not exist in model", request.ServiceId))
			return &discovery.DeleteServiceTagsResponse{
				Response: discovery.CreateResponse(discovery.ErrServiceNotExists, "Service does not exist"),
			}, nil
		}
		log.Error(fmt.Sprintf("failed to get service %s tags", request.ServiceId), err)
		return &discovery.DeleteServiceTagsResponse{
			Response: discovery.CreateResponse(discovery.ErrInternal, err.Error()),
		}, nil
	}
	dataTags := svc.Tags
	newTags := make(map[string]string, len(dataTags))
	for k, v := range dataTags {
		newTags[k] = v
	}
	if len(dataTags) > 0 {
		for _, key := range request.Keys {
			if _, ok := dataTags[key]; !ok {
				return &discovery.DeleteServiceTagsResponse{
					Response: discovery.CreateResponse(discovery.ErrTagNotExists, "Tag does not exist"),
				}, nil
			}
			delete(newTags, key)
		}
	}
	err = mutil.UpdateService(ctx, mutil.GeneratorServiceFilter(ctx, request.ServiceId), bson.M{"$set": bson.M{model.ColumnTag: newTags}})
	if err != nil {
		log.Error(fmt.Sprintf("delete service %s tags failed", request.ServiceId), err)
		return &discovery.DeleteServiceTagsResponse{
			Response: discovery.CreateResponse(discovery.ErrInternal, err.Error()),
		}, nil
	}
	return &discovery.DeleteServiceTagsResponse{
		Response: discovery.CreateResponse(discovery.ResponseSuccess, "Update service tag success."),
	}, nil
}

func (ds *DataSource) GetSchema(ctx context.Context, request *discovery.GetSchemaRequest) (*discovery.GetSchemaResponse, error) {
	exist, err := mutil.ServiceExistID(ctx, request.ServiceId)
	if err != nil {
		return &discovery.GetSchemaResponse{
			Response: discovery.CreateResponse(discovery.ErrInternal, "GetSchema failed to check service exist."),
		}, nil
	}
	if !exist {
		return &discovery.GetSchemaResponse{
			Response: discovery.CreateResponse(discovery.ErrServiceNotExists, "GetSchema service does not exist."),
		}, nil
	}
	schema, err := mutil.GetSchema(ctx, mutil.GeneratorSchemaFilter(ctx, request.ServiceId, request.SchemaId))
	if err != nil {
		return &discovery.GetSchemaResponse{
			Response: discovery.CreateResponse(discovery.ErrInternal, "GetSchema failed from mongodb."),
		}, nil
	}
	if schema == nil {
		return &discovery.GetSchemaResponse{
			Response: discovery.CreateResponse(discovery.ErrSchemaNotExists, "Do not have this schema info."),
		}, nil
	}
	return &discovery.GetSchemaResponse{
		Response:      discovery.CreateResponse(discovery.ResponseSuccess, "Get schema info successfully."),
		Schema:        schema.Schema,
		SchemaSummary: schema.SchemaSummary,
	}, nil
}

func (ds *DataSource) GetAllSchemas(ctx context.Context, request *discovery.GetAllSchemaRequest) (*discovery.GetAllSchemaResponse, error) {
	svc, err := mutil.GetService(ctx, mutil.GeneratorServiceFilter(ctx, request.ServiceId))
	if err != nil {
		if errors.Is(err, datasource.ErrNoData) {
			log.Debug(fmt.Sprintf("service %s not exist in model", request.ServiceId))
			return &discovery.GetAllSchemaResponse{
				Response: discovery.CreateResponse(discovery.ErrServiceNotExists, "GetAllSchemas failed for service not exist"),
			}, nil
		}
		log.Error(fmt.Sprintf("get service[%s] all schemas failed, get service failed", request.ServiceId), err)
		return &discovery.GetAllSchemaResponse{
			Response: discovery.CreateResponse(discovery.ErrInternal, err.Error()),
		}, err
	}
	schemasList := svc.Service.Schemas
	if len(schemasList) == 0 {
		return &discovery.GetAllSchemaResponse{
			Response: discovery.CreateResponse(discovery.ResponseSuccess, "Do not have this schema info."),
			Schemas:  []*discovery.Schema{},
		}, nil
	}
	schemas := make([]*discovery.Schema, 0, len(schemasList))
	for _, schemaID := range schemasList {
		tempSchema := &discovery.Schema{}
		tempSchema.SchemaId = schemaID
		schema, err := mutil.GetSchema(ctx, mutil.GeneratorSchemaFilter(ctx, request.ServiceId, schemaID))
		if err != nil {
			return &discovery.GetAllSchemaResponse{
				Response: discovery.CreateResponse(discovery.ErrInternal, err.Error()),
			}, err
		}
		if schema == nil {
			schemas = append(schemas, tempSchema)
			continue
		}
		tempSchema.Summary = schema.SchemaSummary
		if request.WithSchema {
			tempSchema.Schema = schema.Schema
		}
		schemas = append(schemas, tempSchema)
	}
	return &discovery.GetAllSchemaResponse{
		Response: discovery.CreateResponse(discovery.ResponseSuccess, "Get all schema info successfully."),
		Schemas:  schemas,
	}, nil
}

func (ds *DataSource) ExistSchema(ctx context.Context, request *discovery.GetExistenceRequest) (*discovery.GetExistenceResponse, error) {
	exist, err := mutil.ServiceExistID(ctx, request.ServiceId)
	if err != nil {
		return &discovery.GetExistenceResponse{
			Response: discovery.CreateResponse(discovery.ErrServiceNotExists, "ExistSchema failed for get service failed"),
		}, nil
	}
	if !exist {
		return &discovery.GetExistenceResponse{
			Response: discovery.CreateResponse(discovery.ErrServiceNotExists, "ExistSchema failed for service not exist"),
		}, nil
	}
	Schema, err := mutil.GetSchema(ctx, mutil.GeneratorSchemaFilter(ctx, request.ServiceId, request.SchemaId))
	if err != nil {
		return &discovery.GetExistenceResponse{
			Response: discovery.CreateResponse(discovery.ErrInternal, "ExistSchema failed for get schema failed."),
		}, nil
	}
	if Schema == nil {
		return &discovery.GetExistenceResponse{
			Response: discovery.CreateResponse(discovery.ErrSchemaNotExists, "ExistSchema failed for schema not exist."),
		}, nil
	}
	return &discovery.GetExistenceResponse{
		Response:  discovery.CreateResponse(discovery.ResponseSuccess, "Schema exist."),
		Summary:   Schema.SchemaSummary,
		SchemaId:  Schema.SchemaID,
		ServiceId: Schema.ServiceID,
	}, nil
}

func (ds *DataSource) DeleteSchema(ctx context.Context, request *discovery.DeleteSchemaRequest) (*discovery.DeleteSchemaResponse, error) {
	exist, err := mutil.ServiceExistID(ctx, request.ServiceId)
	if err != nil {
		return &discovery.DeleteSchemaResponse{
			Response: discovery.CreateResponse(discovery.ErrServiceNotExists, "DeleteSchema failed for get service failed."),
		}, nil
	}
	if !exist {
		return &discovery.DeleteSchemaResponse{
			Response: discovery.CreateResponse(discovery.ErrServiceNotExists, "DeleteSchema failed for service not exist."),
		}, nil
	}
	filter := mutil.GeneratorSchemaFilter(ctx, request.ServiceId, request.SchemaId)
	res, err := client.GetMongoClient().DocDelete(ctx, model.CollectionSchema, filter)
	if err != nil {
		return &discovery.DeleteSchemaResponse{
			Response: discovery.CreateResponse(discovery.ErrUnavailableBackend, "DeleteSchema failed for delete schema failed."),
		}, err
	}
	if !res {
		return &discovery.DeleteSchemaResponse{
			Response: discovery.CreateResponse(discovery.ErrSchemaNotExists, "DeleteSchema failed for schema not exist."),
		}, nil
	}
	return &discovery.DeleteSchemaResponse{
		Response: discovery.CreateResponse(discovery.ResponseSuccess, "Delete schema info successfully."),
	}, nil
}

func (ds *DataSource) ModifySchema(ctx context.Context, request *discovery.ModifySchemaRequest) (*discovery.ModifySchemaResponse, error) {
	remoteIP := util.GetIPFromContext(ctx)
	serviceID := request.ServiceId
	schemaID := request.SchemaId
	schema := discovery.Schema{
		SchemaId: request.SchemaId,
		Summary:  request.Summary,
		Schema:   request.Schema,
	}
	err := ds.modifySchema(ctx, request.ServiceId, &schema)
	if err != nil {
		log.Error(fmt.Sprintf("modify schema[%s/%s] failed, operator: %s", serviceID, schemaID, remoteIP), err)
		resp := &discovery.ModifySchemaResponse{
			Response: discovery.CreateResponseWithSCErr(err),
		}
		if err.InternalError() {
			return resp, err
		}
		return resp, nil
	}
	log.Info(fmt.Sprintf("modify schema[%s/%s] successfully, operator: %s", serviceID, schemaID, remoteIP))
	return &discovery.ModifySchemaResponse{
		Response: discovery.CreateResponse(discovery.ResponseSuccess, "modify schema info success."),
	}, nil
}

func (ds *DataSource) ModifySchemas(ctx context.Context, request *discovery.ModifySchemasRequest) (*discovery.ModifySchemasResponse, error) {
	svc, err := mutil.GetService(ctx, mutil.GeneratorServiceFilter(ctx, request.ServiceId))
	if err != nil {
		if errors.Is(err, datasource.ErrNoData) {
			return &discovery.ModifySchemasResponse{Response: discovery.CreateResponse(discovery.ErrServiceNotExists, "Service not exist")}, nil
		}
		return &discovery.ModifySchemasResponse{Response: discovery.CreateResponse(discovery.ErrInternal, err.Error())}, err
	}
	respErr := ds.modifySchemas(ctx, svc.Service, request.Schemas)
	if respErr != nil {
		resp := &discovery.ModifySchemasResponse{
			Response: discovery.CreateResponseWithSCErr(respErr),
		}
		if respErr.InternalError() {
			return resp, err
		}
		return resp, nil
	}
	return &discovery.ModifySchemasResponse{
		Response: discovery.CreateResponse(discovery.ResponseSuccess, "modify schemas info success"),
	}, nil

}

func (ds *DataSource) modifySchemas(ctx context.Context, service *discovery.MicroService, schemas []*discovery.Schema) *discovery.Error {
	domain := util.ParseDomain(ctx)
	project := util.ParseProject(ctx)
	remoteIP := util.GetIPFromContext(ctx)

	serviceID := service.ServiceId
	schemasFromDatabase, err := mutil.GetSchemas(ctx, bson.M{model.ColumnServiceID: serviceID})
	if err != nil {
		log.Error(fmt.Sprintf("modify service %s schemas failed, get schemas failed, operator: %s", serviceID, remoteIP), err)
		return discovery.NewError(discovery.ErrUnavailableBackend, err.Error())
	}

	needUpdateSchemas, needAddSchemas, needDeleteSchemas, nonExistSchemaIds :=
		datasource.SchemasAnalysis(schemas, schemasFromDatabase, service.Schemas)

	var schemasOps []mongo.WriteModel
	var serviceOps []mongo.WriteModel
	if !ds.isSchemaEditable(service) {
		if len(service.Schemas) == 0 {
			res := quota.NewApplyQuotaResource(quota.SchemaQuotaType, util.ParseDomainProject(ctx), serviceID, int64(len(nonExistSchemaIds)))
			rst := quota.Apply(ctx, res)
			errQuota := rst.Err
			if errQuota != nil {
				log.Error(fmt.Sprintf("modify service[%s] schemas failed, operator: %s", serviceID, remoteIP), errQuota)
				return errQuota
			}
			serviceOps = append(serviceOps, mongo.NewUpdateOneModel().SetUpdate(bson.M{"$set": bson.M{mutil.StringBuilder([]string{model.ColumnService, model.ColumnSchemas}): nonExistSchemaIds}}).SetFilter(mutil.GeneratorServiceFilter(ctx, serviceID)))
		} else {
			if len(nonExistSchemaIds) != 0 {
				errInfo := fmt.Errorf("non-existent schemaIDs %v", nonExistSchemaIds)
				log.Error(fmt.Sprintf("modify service %s schemas failed, operator: %s", serviceID, remoteIP), err)
				return discovery.NewError(discovery.ErrUndefinedSchemaID, errInfo.Error())
			}
			for _, needUpdateSchema := range needUpdateSchemas {
				exist, err := mutil.SchemaSummaryExist(ctx, serviceID, needUpdateSchema.SchemaId)
				if err != nil {
					return discovery.NewError(discovery.ErrInternal, err.Error())
				}
				if !exist {
					schemasOps = append(schemasOps, mongo.NewUpdateOneModel().SetFilter(mutil.GeneratorSchemaFilter(ctx, serviceID, needUpdateSchema.SchemaId)).SetUpdate(bson.M{"$set": bson.M{model.ColumnSchema: needUpdateSchema.Schema, model.ColumnSchemaSummary: needUpdateSchema.Summary}}))
				} else {
					log.Warn(fmt.Sprintf("schema[%s/%s] and it's summary already exist, skip to update, operator: %s",
						serviceID, needUpdateSchema.SchemaId, remoteIP))
				}
			}
		}

		for _, schema := range needAddSchemas {
			log.Info(fmt.Sprintf("add new schema[%s/%s], operator: %s", serviceID, schema.SchemaId, remoteIP))
			schemasOps = append(schemasOps, mongo.NewInsertOneModel().SetDocument(&model.Schema{
				Domain:        domain,
				Project:       project,
				ServiceID:     serviceID,
				SchemaID:      schema.SchemaId,
				Schema:        schema.Schema,
				SchemaSummary: schema.Summary,
			}))
		}
	} else {
		quotaSize := len(needAddSchemas) - len(needDeleteSchemas)
		if quotaSize > 0 {
			res := quota.NewApplyQuotaResource(quota.SchemaQuotaType, util.ParseDomainProject(ctx), serviceID, int64(quotaSize))
			rst := quota.Apply(ctx, res)
			err := rst.Err
			if err != nil {
				log.Error(fmt.Sprintf("modify service[%s] schemas failed, operator: %s", serviceID, remoteIP), err)
				return err
			}
		}
		var schemaIDs []string
		for _, schema := range needAddSchemas {
			log.Info(fmt.Sprintf("add new schema[%s/%s], operator: %s", serviceID, schema.SchemaId, remoteIP))
			schemasOps = append(schemasOps, mongo.NewInsertOneModel().SetDocument(&model.Schema{
				Domain:        domain,
				Project:       project,
				ServiceID:     serviceID,
				SchemaID:      schema.SchemaId,
				Schema:        schema.Schema,
				SchemaSummary: schema.Summary,
			}))
			schemaIDs = append(schemaIDs, schema.SchemaId)
		}

		for _, schema := range needUpdateSchemas {
			log.Info(fmt.Sprintf("update schema[%s/%s], operator: %s", serviceID, schema.SchemaId, remoteIP))
			schemasOps = append(schemasOps, mongo.NewUpdateOneModel().SetFilter(mutil.GeneratorSchemaFilter(ctx, serviceID, schema.SchemaId)).SetUpdate(bson.M{"$set": bson.M{model.ColumnSchema: schema.Schema, model.ColumnSchemaSummary: schema.Summary}}))
			schemaIDs = append(schemaIDs, schema.SchemaId)
		}

		for _, schema := range needDeleteSchemas {
			log.Info(fmt.Sprintf("delete non-existent schema[%s/%s], operator: %s", serviceID, schema.SchemaId, remoteIP))
			schemasOps = append(schemasOps, mongo.NewDeleteOneModel().SetFilter(mutil.GeneratorSchemaFilter(ctx, serviceID, schema.SchemaId)))
		}

		serviceOps = append(serviceOps, mongo.NewUpdateOneModel().SetUpdate(bson.M{"$set": bson.M{mutil.StringBuilder([]string{model.ColumnService, model.ColumnSchemas}): schemaIDs}}).SetFilter(mutil.GeneratorServiceFilter(ctx, serviceID)))
	}
	if len(schemasOps) > 0 {
		_, err = client.GetMongoClient().BatchUpdate(ctx, model.CollectionSchema, schemasOps)
		if err != nil {
			return discovery.NewError(discovery.ErrInternal, err.Error())
		}
	}
	if len(serviceOps) > 0 {
		_, err = client.GetMongoClient().BatchUpdate(ctx, model.CollectionService, serviceOps)
		if err != nil {
			return discovery.NewError(discovery.ErrInternal, err.Error())
		}
	}
	return nil
}

// modifySchema will be modified in the following cases
// 1.service have no relation --> update the schema && update the service
// 2.service is editable && service have relation with the schema --> update the shema
// 3.service is editable && service have no relation with the schema --> update the schema && update the service
// 4.service can't edit && service have relation with the schema && schema summary not exist --> update the schema
func (ds *DataSource) modifySchema(ctx context.Context, serviceID string, schema *discovery.Schema) *discovery.Error {
	remoteIP := util.GetIPFromContext(ctx)
	svc, err := mutil.GetService(ctx, mutil.GeneratorServiceFilter(ctx, serviceID))
	if err != nil {
		if errors.Is(err, datasource.ErrNoData) {
			return discovery.NewError(discovery.ErrServiceNotExists, "Service does not exist.")
		}
		return discovery.NewError(discovery.ErrInternal, err.Error())
	}
	microservice := svc.Service
	var isExist bool
	for _, sid := range microservice.Schemas {
		if sid == schema.SchemaId {
			isExist = true
			break
		}
	}
	var newSchemas []string
	if !ds.isSchemaEditable(microservice) {
		if len(microservice.Schemas) != 0 && !isExist {
			return discovery.NewError(discovery.ErrUndefinedSchemaID, "Non-existent schemaID can't be added request "+discovery.ENV_PROD)
		}
		respSchema, err := mutil.GetSchema(ctx, mutil.GeneratorSchemaFilter(ctx, serviceID, schema.SchemaId))
		if err != nil {
			return discovery.NewError(discovery.ErrUnavailableBackend, err.Error())
		}
		if respSchema != nil {
			if len(schema.Summary) == 0 {
				log.Error(fmt.Sprintf("modify schema %s %s failed, get schema summary failed, operator: %s",
					serviceID, schema.SchemaId, remoteIP), err)
				return discovery.NewError(discovery.ErrModifySchemaNotAllow,
					"schema already exist, can not be changed request "+discovery.ENV_PROD)
			}
			if len(respSchema.SchemaSummary) != 0 {
				log.Error(fmt.Sprintf("mode, schema %s %s already exist, can not be changed, operator: %s",
					serviceID, schema.SchemaId, remoteIP), err)
				return discovery.NewError(discovery.ErrModifySchemaNotAllow, "schema already exist, can not be changed request "+discovery.ENV_PROD)
			}
		}
		if len(microservice.Schemas) == 0 {
			copy(newSchemas, microservice.Schemas)
			newSchemas = append(newSchemas, schema.SchemaId)
		}
	} else {
		if !isExist {
			copy(newSchemas, microservice.Schemas)
			newSchemas = append(newSchemas, schema.SchemaId)
		}
	}
	if len(newSchemas) != 0 {
		updateData := bson.M{mutil.StringBuilder([]string{model.ColumnService, model.ColumnSchemas}): newSchemas}
		err = mutil.UpdateService(ctx, mutil.GeneratorServiceFilter(ctx, serviceID), bson.M{"$set": updateData})
		if err != nil {
			return discovery.NewError(discovery.ErrInternal, err.Error())
		}
	}
	newSchema := bson.M{"$set": bson.M{model.ColumnSchema: schema.Schema, model.ColumnSchemaSummary: schema.Summary}}
	err = mutil.UpdateSchema(ctx, mutil.GeneratorSchemaFilter(ctx, serviceID, schema.SchemaId), newSchema, options.FindOneAndUpdate().SetUpsert(true))
	if err != nil {
		return discovery.NewError(discovery.ErrInternal, err.Error())
	}
	return nil
}

func (ds *DataSource) AddRule(ctx context.Context, request *discovery.AddServiceRulesRequest) (*discovery.AddServiceRulesResponse, error) {
	remoteIP := util.GetIPFromContext(ctx)
	exist, err := mutil.ServiceExistID(ctx, request.ServiceId)
	if err != nil {
		log.Error(fmt.Sprintf("failed to add rules for service %s for get service failed", request.ServiceId), err)
		return &discovery.AddServiceRulesResponse{
			Response: discovery.CreateResponse(discovery.ErrInternal, "Failed to check service exist"),
		}, nil
	}
	if !exist {
		return &discovery.AddServiceRulesResponse{Response: discovery.CreateResponse(discovery.ErrServiceNotExists, "Service does not exist")}, nil
	}
	res := quota.NewApplyQuotaResource(quota.RuleQuotaType, util.ParseDomainProject(ctx), request.ServiceId, int64(len(request.Rules)))
	rst := quota.Apply(ctx, res)
	errQuota := rst.Err
	if errQuota != nil {
		log.Error(fmt.Sprintf("add service[%s] rule failed, operator: %s", request.ServiceId, remoteIP), errQuota)
		response := &discovery.AddServiceRulesResponse{
			Response: discovery.CreateResponseWithSCErr(errQuota),
		}
		if errQuota.InternalError() {
			return response, errQuota
		}
		return response, nil
	}
	rules, err := mutil.GetRules(ctx, request.ServiceId)
	if err != nil {
		return &discovery.AddServiceRulesResponse{
			Response: discovery.CreateResponse(discovery.ErrInternal, err.Error()),
		}, err
	}
	var ruleType string
	if len(rules) != 0 {
		ruleType = rules[0].RuleType
	}
	ruleIDs := make([]string, 0, len(request.Rules))
	for _, rule := range request.Rules {
		if len(ruleType) == 0 {
			ruleType = rule.RuleType
		} else if ruleType != rule.RuleType {
			return &discovery.AddServiceRulesResponse{
				Response: discovery.CreateResponse(discovery.ErrBlackAndWhiteRule, "Service can only contain one rule type,Black or white."),
			}, nil
		}
		//the rule unique index is (serviceid,attribute,pattern)
		exist, err := mutil.RuleExist(ctx, mutil.GeneratorRuleAttFilter(ctx, request.ServiceId, rule.Attribute, rule.Pattern))
		if err != nil {
			return &discovery.AddServiceRulesResponse{
				Response: discovery.CreateResponse(discovery.ErrUnavailableBackend, "Can not check rule if exist."),
			}, nil
		}
		if exist {
			continue
		}
		timestamp := strconv.FormatInt(time.Now().Unix(), 10)
		ruleAdd := &model.Rule{
			Domain:    util.ParseDomain(ctx),
			Project:   util.ParseProject(ctx),
			ServiceID: request.ServiceId,
			Rule: &discovery.ServiceRule{
				RuleId:       util.GenerateUUID(),
				RuleType:     rule.RuleType,
				Attribute:    rule.Attribute,
				Pattern:      rule.Pattern,
				Description:  rule.Description,
				Timestamp:    timestamp,
				ModTimestamp: timestamp,
			},
		}
		ruleIDs = append(ruleIDs, ruleAdd.Rule.RuleId)
		_, err = client.GetMongoClient().Insert(ctx, model.CollectionRule, ruleAdd)
		if err != nil {
			return &discovery.AddServiceRulesResponse{
				Response: discovery.CreateResponse(discovery.ErrInternal, err.Error()),
			}, err
		}
	}
	return &discovery.AddServiceRulesResponse{
		Response: discovery.CreateResponse(discovery.ResponseSuccess, "Add service rules successfully."),
		RuleIds:  ruleIDs,
	}, nil
}

func (ds *DataSource) GetRules(ctx context.Context, request *discovery.GetServiceRulesRequest) (*discovery.GetServiceRulesResponse, error) {
	exist, err := mutil.ServiceExistID(ctx, request.ServiceId)
	if err != nil {
		return &discovery.GetServiceRulesResponse{
			Response: discovery.CreateResponse(discovery.ErrServiceNotExists, "GetRules failed for get service failed."),
		}, nil
	}
	if !exist {
		return &discovery.GetServiceRulesResponse{
			Response: discovery.CreateResponse(discovery.ErrServiceNotExists, "GetRules failed for service not exist."),
		}, nil
	}
	rules, err := mutil.GetRules(ctx, request.ServiceId)
	if err != nil {
		return &discovery.GetServiceRulesResponse{
			Response: discovery.CreateResponse(discovery.ErrInternal, err.Error()),
		}, nil
	}
	return &discovery.GetServiceRulesResponse{
		Response: discovery.CreateResponse(discovery.ResponseSuccess, "Get service rules successfully."),
		Rules:    rules,
	}, nil
}

func (ds *DataSource) DeleteRule(ctx context.Context, request *discovery.DeleteServiceRulesRequest) (*discovery.DeleteServiceRulesResponse, error) {
	exist, err := mutil.ServiceExistID(ctx, request.ServiceId)
	if err != nil {
		log.Error(fmt.Sprintf("failed to add tags for service %s for get service failed", request.ServiceId), err)
		return &discovery.DeleteServiceRulesResponse{
			Response: discovery.CreateResponse(discovery.ErrInternal, "Failed to check service exist"),
		}, err
	}
	if !exist {
		return &discovery.DeleteServiceRulesResponse{Response: discovery.CreateResponse(discovery.ErrServiceNotExists, "Service not exist")}, nil
	}
	var delRules []mongo.WriteModel
	for _, ruleID := range request.RuleIds {
		exist, err := mutil.RuleExist(ctx, mutil.GeneratorRuleFilter(ctx, request.ServiceId, ruleID))
		if err != nil {
			return &discovery.DeleteServiceRulesResponse{
				Response: discovery.CreateResponse(discovery.ErrInternal, err.Error()),
			}, err
		}
		if !exist {
			return &discovery.DeleteServiceRulesResponse{
				Response: discovery.CreateResponse(discovery.ErrRuleNotExists, "This rule does not exist."),
			}, nil
		}
		delRules = append(delRules, mongo.NewDeleteOneModel().SetFilter(mutil.GeneratorRuleFilter(ctx, request.ServiceId, ruleID)))
	}
	if len(delRules) > 0 {
		_, err := client.GetMongoClient().BatchDelete(ctx, model.CollectionRule, delRules)
		if err != nil {
			return &discovery.DeleteServiceRulesResponse{
				Response: discovery.CreateResponse(discovery.ErrInternal, err.Error()),
			}, err
		}
	}
	return &discovery.DeleteServiceRulesResponse{
		Response: discovery.CreateResponse(discovery.ResponseSuccess, "Delete service rules successfully."),
	}, nil
}

func (ds *DataSource) UpdateRule(ctx context.Context, request *discovery.UpdateServiceRuleRequest) (*discovery.UpdateServiceRuleResponse, error) {
	exist, err := mutil.ServiceExistID(ctx, request.ServiceId)
	if err != nil {
		return &discovery.UpdateServiceRuleResponse{
			Response: discovery.CreateResponse(discovery.ErrServiceNotExists, "UpdateRule failed for get service failed."),
		}, nil
	}
	if !exist {
		return &discovery.UpdateServiceRuleResponse{
			Response: discovery.CreateResponse(discovery.ErrServiceNotExists, "UpdateRule failed for service not exist."),
		}, nil
	}
	rules, err := mutil.GetRules(ctx, request.ServiceId)
	if err != nil {
		return &discovery.UpdateServiceRuleResponse{
			Response: discovery.CreateResponse(discovery.ErrUnavailableBackend, "UpdateRule failed for get rule."),
		}, nil
	}
	if len(rules) >= 1 && rules[0].RuleType != request.Rule.RuleType {
		return &discovery.UpdateServiceRuleResponse{
			Response: discovery.CreateResponse(discovery.ErrModifyRuleNotAllow, "Exist multiple rules, can not change rule type. Rule type is ."+rules[0].RuleType),
		}, nil
	}
	exist, err = mutil.RuleExist(ctx, mutil.GeneratorRuleFilter(ctx, request.ServiceId, request.RuleId))
	if err != nil {
		return &discovery.UpdateServiceRuleResponse{
			Response: discovery.CreateResponse(discovery.ErrInternal, err.Error()),
		}, nil
	}
	if !exist {
		return &discovery.UpdateServiceRuleResponse{
			Response: discovery.CreateResponse(discovery.ErrRuleNotExists, "This rule does not exist."),
		}, nil
	}

	newRule := bson.M{
		mutil.StringBuilder([]string{model.ColumnRule, model.ColumnRuleType}):    request.Rule.RuleType,
		mutil.StringBuilder([]string{model.ColumnRule, model.ColumnPattern}):     request.Rule.Pattern,
		mutil.StringBuilder([]string{model.ColumnRule, model.ColumnAttribute}):   request.Rule.Attribute,
		mutil.StringBuilder([]string{model.ColumnRule, model.ColumnDescription}): request.Rule.Description,
		mutil.StringBuilder([]string{model.ColumnRule, model.ColumnModTime}):     strconv.FormatInt(time.Now().Unix(), 10)}

	err = mutil.UpdateRule(ctx, mutil.GeneratorRuleFilter(ctx, request.ServiceId, request.RuleId), bson.M{"$set": newRule})
	if err != nil {
		return &discovery.UpdateServiceRuleResponse{
			Response: discovery.CreateResponse(discovery.ErrInternal, err.Error()),
		}, err
	}
	return &discovery.UpdateServiceRuleResponse{
		Response: discovery.CreateResponse(discovery.ResponseSuccess, "Update service rules succesfully."),
	}, nil
}

func (ds *DataSource) isSchemaEditable(service *discovery.MicroService) bool {
	return (len(service.Environment) != 0 && service.Environment != discovery.ENV_PROD) || ds.SchemaEditable
}

// Instance management
func (ds *DataSource) RegisterInstance(ctx context.Context, request *discovery.RegisterInstanceRequest) (*discovery.RegisterInstanceResponse, error) {
	remoteIP := util.GetIPFromContext(ctx)
	instance := request.Instance

	// 允许自定义 id
	if len(instance.InstanceId) > 0 {
		resp, err := ds.Heartbeat(ctx, &discovery.HeartbeatRequest{
			InstanceId: instance.InstanceId,
			ServiceId:  instance.ServiceId,
		})
		if err != nil || resp == nil {
			log.Error(fmt.Sprintf("register service %s's instance failed, endpoints %s, host '%s', operator %s",
				instance.ServiceId, instance.Endpoints, instance.HostName, remoteIP), err)
			return &discovery.RegisterInstanceResponse{
				Response: discovery.CreateResponse(discovery.ErrInternal, err.Error()),
			}, nil
		}
		switch resp.Response.GetCode() {
		case discovery.ResponseSuccess:
			log.Info(fmt.Sprintf("register instance successful, reuse instance[%s/%s], operator %s",
				instance.ServiceId, instance.InstanceId, remoteIP))
			return &discovery.RegisterInstanceResponse{
				Response:   resp.Response,
				InstanceId: instance.InstanceId,
			}, nil
		case discovery.ErrInstanceNotExists:
			// register a new one
			if request.Instance.HealthCheck == nil {
				request.Instance.HealthCheck = &discovery.HealthCheck{
					Mode:     discovery.CHECK_BY_HEARTBEAT,
					Interval: apt.RegistryDefaultLeaseRenewalinterval,
					Times:    apt.RegistryDefaultLeaseRetrytimes,
				}
			}
			return mutil.RegistryInstance(ctx, request)
		default:
			log.Error(fmt.Sprintf("register instance failed, reuse instance %s %s, operator %s",
				instance.ServiceId, instance.InstanceId, remoteIP), err)
			return &discovery.RegisterInstanceResponse{
				Response: resp.Response,
			}, err
		}
	}

	if err := mutil.PreProcessRegisterInstance(ctx, instance); err != nil {
		log.Error(fmt.Sprintf("register service %s instance failed, endpoints %s, host %s operator %s",
			instance.ServiceId, instance.Endpoints, instance.HostName, remoteIP), err)
		return &discovery.RegisterInstanceResponse{
			Response: discovery.CreateResponseWithSCErr(err),
		}, nil
	}
	return mutil.RegistryInstance(ctx, request)
}

// GetInstance returns instance under the current domain
func (ds *DataSource) GetInstance(ctx context.Context, request *discovery.GetOneInstanceRequest) (*discovery.GetOneInstanceResponse, error) {
	var service *model.Service
	var err error
	var serviceIDs []string
	if len(request.ConsumerServiceId) > 0 {
		filter := mutil.GeneratorServiceFilter(ctx, request.ConsumerServiceId)
		service, err = mutil.GetService(ctx, filter)
		if err != nil {
			if errors.Is(err, datasource.ErrNoData) {
				log.Debug(fmt.Sprintf("consumer does not exist, consumer %s find provider instance %s %s",
					request.ConsumerServiceId, request.ProviderServiceId, request.ProviderInstanceId))
				return &discovery.GetOneInstanceResponse{
					Response: discovery.CreateResponse(discovery.ErrServiceNotExists,
						fmt.Sprintf("Consumer[%s] does not exist.", request.ConsumerServiceId)),
				}, nil
			}
			log.Error(fmt.Sprintf(" get consumer failed, consumer %s find provider instance %s",
				request.ConsumerServiceId, request.ProviderInstanceId), err)
			return &discovery.GetOneInstanceResponse{
				Response: discovery.CreateResponse(discovery.ErrInternal, err.Error()),
			}, err
		}
	}

	filter := mutil.GeneratorServiceFilter(ctx, request.ProviderServiceId)
	provider, err := mutil.GetService(ctx, filter)
	if err != nil {
		if errors.Is(err, datasource.ErrNoData) {
			log.Debug(fmt.Sprintf("provider does not exist, consumer %s find provider instance %s %s",
				request.ConsumerServiceId, request.ProviderServiceId, request.ProviderInstanceId))
			return &discovery.GetOneInstanceResponse{
				Response: discovery.CreateResponse(discovery.ErrServiceNotExists,
					fmt.Sprintf("Provider[%s] does not exist.", request.ProviderServiceId)),
			}, nil
		}
		log.Error(fmt.Sprintf("get provider failed, consumer %s find provider instance %s %s",
			request.ConsumerServiceId, request.ProviderServiceId, request.ProviderInstanceId), err)
		return &discovery.GetOneInstanceResponse{
			Response: discovery.CreateResponse(discovery.ErrInternal, err.Error()),
		}, err
	}
	findFlag := func() string {
		return fmt.Sprintf("consumer[%s][%s/%s/%s/%s] find provider[%s][%s/%s/%s/%s] instance[%s]",
			request.ConsumerServiceId, service.Service.Environment, service.Service.AppId, service.Service.ServiceName, service.Service.Version,
			provider.Service.ServiceId, provider.Service.Environment, provider.Service.AppId, provider.Service.ServiceName, provider.Service.Version,
			request.ProviderInstanceId)
	}

	domainProject := util.ParseDomainProject(ctx)
	services, err := mutil.FindServices(ctx, discovery.MicroServiceToKey(domainProject, provider.Service))
	if err != nil {
		log.Error(fmt.Sprintf("get instance failed %s", findFlag()), err)
		return &discovery.GetOneInstanceResponse{
			Response: discovery.CreateResponse(discovery.ErrInternal, err.Error()),
		}, err
	}
	rev, _ := ctx.Value(util.CtxRequestRevision).(string)
	serviceIDs = mutil.FilterServiceIDs(ctx, request.ConsumerServiceId, request.Tags, services)
	if len(serviceIDs) == 0 {
		mes := fmt.Errorf("%s failed, provider instance does not exist", findFlag())
		log.Error("query service failed", mes)
		return &discovery.GetOneInstanceResponse{
			Response: discovery.CreateResponse(discovery.ErrInstanceNotExists, mes.Error()),
		}, nil
	}
	instances, err := mutil.GetInstancesByServiceID(ctx, request.ProviderServiceId)
	if err != nil {
		log.Error(fmt.Sprintf("get instance failed %s", findFlag()), err)
		return &discovery.GetOneInstanceResponse{
			Response: discovery.CreateResponse(discovery.ErrInternal, err.Error()),
		}, err
	}
	instance := instances[0]
	// use explicit instanceId to query
	if len(request.ProviderInstanceId) != 0 {
		isExist := false
		for _, ins := range instances {
			if ins.InstanceId == request.ProviderInstanceId {
				instance = ins
				isExist = true
				break
			}
		}
		if !isExist {
			mes := fmt.Errorf("%s failed, provider instance does not exist", findFlag())
			log.Error("get instance failed", mes)
			return &discovery.GetOneInstanceResponse{
				Response: discovery.CreateResponse(discovery.ErrInstanceNotExists, mes.Error()),
			}, nil
		}
	}

	newRev, _ := mutil.FormatRevision(request.ConsumerServiceId, instances)
	if rev == newRev {
		instance = nil // for gRPC
	}
	// TODO support gRPC output context
	_ = util.WithResponseRev(ctx, newRev)

	return &discovery.GetOneInstanceResponse{
		Response: discovery.CreateResponse(discovery.ResponseSuccess, "Get instance successfully."),
		Instance: instance,
	}, nil
}

func (ds *DataSource) GetInstances(ctx context.Context, request *discovery.GetInstancesRequest) (*discovery.GetInstancesResponse, error) {
	service := &model.Service{}
	var err error

	if len(request.ConsumerServiceId) > 0 {
		service, err = mutil.GetServiceByID(ctx, request.ConsumerServiceId)
		if err != nil {
			if errors.Is(err, datasource.ErrNoData) {
				log.Debug(fmt.Sprintf("consumer does not exist, consumer %s find provider %s instances",
					request.ConsumerServiceId, request.ProviderServiceId))
				return &discovery.GetInstancesResponse{
					Response: discovery.CreateResponse(discovery.ErrServiceNotExists,
						fmt.Sprintf("Consumer[%s] does not exist.", request.ConsumerServiceId)),
				}, nil
			}
			log.Error(fmt.Sprintf("get consumer failed, consumer %s find provider %s instances",
				request.ConsumerServiceId, request.ProviderServiceId), err)
			return &discovery.GetInstancesResponse{
				Response: discovery.CreateResponse(discovery.ErrInternal, err.Error()),
			}, err
		}
	}

	provider, err := mutil.GetServiceByID(ctx, request.ProviderServiceId)
	if err != nil {
		if errors.Is(err, datasource.ErrNoData) {
			log.Debug(fmt.Sprintf("provider does not exist, consumer %s find provider %s  instances",
				request.ConsumerServiceId, request.ProviderServiceId))
			return &discovery.GetInstancesResponse{
				Response: discovery.CreateResponse(discovery.ErrServiceNotExists,
					fmt.Sprintf("provider[%s] does not exist.", request.ProviderServiceId)),
			}, nil
		}
		log.Error(fmt.Sprintf("get provider failed, consumer %s find provider instances %s",
			request.ConsumerServiceId, request.ProviderServiceId), err)
		return &discovery.GetInstancesResponse{
			Response: discovery.CreateResponse(discovery.ErrInternal, err.Error()),
		}, err
	}

	findFlag := func() string {
		return fmt.Sprintf("consumer[%s][%s/%s/%s/%s] find provider[%s][%s/%s/%s/%s] instances",
			request.ConsumerServiceId, service.Service.Environment, service.Service.AppId, service.Service.ServiceName, service.Service.Version,
			provider.Service.ServiceId, provider.Service.Environment, provider.Service.AppId, provider.Service.ServiceName, provider.Service.Version)
	}

	rev, _ := ctx.Value(util.CtxRequestRevision).(string)
	serviceIDs := mutil.FilterServiceIDs(ctx, request.ConsumerServiceId, request.Tags, []*model.Service{provider})
	if len(serviceIDs) == 0 {
		mes := fmt.Errorf("%s failed, provider does not exist", findFlag())
		log.Error("query service failed", mes)
		return &discovery.GetInstancesResponse{
			Response: discovery.CreateResponse(discovery.ErrServiceNotExists, mes.Error()),
		}, nil
	}
	instances, err := mutil.GetInstancesByServiceID(ctx, request.ProviderServiceId)
	if err != nil {
		log.Error(fmt.Sprintf("get instances failed %s", findFlag()), err)
		return &discovery.GetInstancesResponse{
			Response: discovery.CreateResponse(discovery.ErrInternal, err.Error()),
		}, err
	}
	newRev, _ := mutil.FormatRevision(request.ConsumerServiceId, instances)
	if rev == newRev {
		instances = nil // for gRPC
	}
	_ = util.WithResponseRev(ctx, newRev)
	return &discovery.GetInstancesResponse{
		Response:  discovery.CreateResponse(discovery.ResponseSuccess, "Query service instances successfully."),
		Instances: instances,
	}, nil
}

// GetProviderInstances returns instances under the specified domain
func (ds *DataSource) GetProviderInstances(ctx context.Context, request *discovery.GetProviderInstancesRequest) (instances []*discovery.MicroServiceInstance, rev string, err error) {
	domain := util.ParseDomain(ctx)
	project := util.ParseProject(ctx)
	filter := bson.M{
		model.ColumnDomain:  domain,
		model.ColumnProject: project,
		mutil.StringBuilder([]string{model.ColumnInstance, model.ColumnServiceID}): request.ProviderServiceId}

	findRes, err := client.GetMongoClient().Find(ctx, model.CollectionInstance, filter)
	if err != nil {
		return
	}

	for findRes.Next(ctx) {
		var mongoInstance model.Instance
		err := findRes.Decode(&mongoInstance)
		if err == nil {
			instances = append(instances, mongoInstance.Instance)
		}
	}

	return instances, "", nil
}

func (ds *DataSource) GetAllInstances(ctx context.Context, request *discovery.GetAllInstancesRequest) (*discovery.GetAllInstancesResponse, error) {

	domain := util.ParseDomain(ctx)
	project := util.ParseProject(ctx)

	filter := bson.M{model.ColumnDomain: domain, model.ColumnProject: project}

	findRes, err := client.GetMongoClient().Find(ctx, model.CollectionInstance, filter)
	if err != nil {
		return nil, err
	}
	resp := &discovery.GetAllInstancesResponse{
		Response: discovery.CreateResponse(discovery.ResponseSuccess, "Get all instances successfully"),
	}

	for findRes.Next(ctx) {
		var instance model.Instance
		err := findRes.Decode(&instance)
		if err != nil {
			return &discovery.GetAllInstancesResponse{
				Response: discovery.CreateResponse(discovery.ErrInternal, err.Error()),
			}, err
		}
		resp.Instances = append(resp.Instances, instance.Instance)
	}

	return resp, nil
}

func (ds *DataSource) BatchGetProviderInstances(ctx context.Context, request *discovery.BatchGetInstancesRequest) (instances []*discovery.MicroServiceInstance, rev string, err error) {
	if request == nil || len(request.ServiceIds) == 0 {
		return nil, "", model.ErrInvalidParamBatchGetInstancesRequest
	}

	domain := util.ParseDomain(ctx)
	project := util.ParseProject(ctx)

	for _, providerServiceID := range request.ServiceIds {
		filter := bson.M{
			model.ColumnDomain:  domain,
			model.ColumnProject: project,
			mutil.StringBuilder([]string{model.ColumnInstance, model.ColumnServiceID}): providerServiceID}
		findRes, err := client.GetMongoClient().Find(ctx, model.CollectionInstance, filter)
		if err != nil {
			return instances, "", nil
		}

		for findRes.Next(ctx) {
			var mongoInstance model.Instance
			err := findRes.Decode(&mongoInstance)
			if err == nil {
				instances = append(instances, mongoInstance.Instance)
			}
		}
	}

	return instances, "", nil
}

// FindInstances returns instances under the specified domain
func (ds *DataSource) FindInstances(ctx context.Context, request *discovery.FindInstancesRequest) (*discovery.FindInstancesResponse, error) {
	provider := &discovery.MicroServiceKey{
		Tenant:      util.ParseTargetDomainProject(ctx),
		Environment: request.Environment,
		AppId:       request.AppId,
		ServiceName: request.ServiceName,
		Alias:       request.Alias,
		Version:     request.VersionRule,
	}
	rev, ok := ctx.Value(util.CtxRequestRevision).(string)
	if !ok {
		err := errors.New("rev request context is not type string")
		log.Error("", err)
		return &discovery.FindInstancesResponse{
			Response: discovery.CreateResponse(discovery.ErrInternal, err.Error()),
		}, err
	}

	if apt.IsGlobal(provider) {
		return ds.findSharedServiceInstance(ctx, request, provider, rev)
	}

	return ds.findInstance(ctx, request, provider, rev)
}

func (ds *DataSource) UpdateInstanceStatus(ctx context.Context, request *discovery.UpdateInstanceStatusRequest) (*discovery.UpdateInstanceStatusResponse, error) {
	updateStatusFlag := util.StringJoin([]string{request.ServiceId, request.InstanceId, request.Status}, "/")

	// todo finish get instance
	instance, err := mutil.GetInstance(ctx, request.ServiceId, request.InstanceId)
	if err != nil {
		log.Error(fmt.Sprintf("update instance %s status failed", updateStatusFlag), err)
		return &discovery.UpdateInstanceStatusResponse{
			Response: discovery.CreateResponse(discovery.ErrInternal, err.Error()),
		}, err
	}
	if instance == nil {
		log.Error(fmt.Sprintf("update instance %s status failed, instance does not exist", updateStatusFlag), err)
		return &discovery.UpdateInstanceStatusResponse{
			Response: discovery.CreateResponse(discovery.ErrInstanceNotExists, "Service instance does not exist."),
		}, nil
	}

	copyInstanceRef := *instance
	copyInstanceRef.Instance.Status = request.Status

	if err := mutil.UpdateInstanceStatus(ctx, copyInstanceRef.Instance); err != nil {
		log.Error(fmt.Sprintf("update instance %s status failed", updateStatusFlag), err)
		resp := &discovery.UpdateInstanceStatusResponse{
			Response: discovery.CreateResponseWithSCErr(err),
		}
		if err.InternalError() {
			return resp, err
		}
		return resp, nil
	}

	log.Info(fmt.Sprintf("update instance[%s] status successfully", updateStatusFlag))
	return &discovery.UpdateInstanceStatusResponse{
		Response: discovery.CreateResponse(discovery.ResponseSuccess, "Update service instance status successfully."),
	}, nil
}

func (ds *DataSource) UpdateInstanceProperties(ctx context.Context, request *discovery.UpdateInstancePropsRequest) (*discovery.UpdateInstancePropsResponse, error) {
	instanceFlag := util.StringJoin([]string{request.ServiceId, request.InstanceId}, "/")

	instance, err := mutil.GetInstance(ctx, request.ServiceId, request.InstanceId)
	if err != nil {
		log.Error(fmt.Sprintf("update instance %s properties failed", instanceFlag), err)
		return &discovery.UpdateInstancePropsResponse{
			Response: discovery.CreateResponse(discovery.ErrInternal, err.Error()),
		}, err
	}
	if instance == nil {
		log.Error(fmt.Sprintf("update instance %s properties failed, instance does not exist", instanceFlag), err)
		return &discovery.UpdateInstancePropsResponse{
			Response: discovery.CreateResponse(discovery.ErrInstanceNotExists, "Service instance does not exist."),
		}, nil
	}

	copyInstanceRef := *instance
	copyInstanceRef.Instance.Properties = request.Properties

	// todo finish update instance
	if err := mutil.UpdateInstanceProperties(ctx, copyInstanceRef.Instance); err != nil {
		log.Error(fmt.Sprintf("update instance %s properties failed", instanceFlag), err)
		resp := &discovery.UpdateInstancePropsResponse{
			Response: discovery.CreateResponseWithSCErr(err),
		}
		if err.InternalError() {
			return resp, err
		}
		return resp, nil
	}

	log.Info(fmt.Sprintf("update instance[%s] properties successfully", instanceFlag))
	return &discovery.UpdateInstancePropsResponse{
		Response: discovery.CreateResponse(discovery.ResponseSuccess, "Update service instance properties successfully."),
	}, nil
}

func (ds *DataSource) UnregisterInstance(ctx context.Context, request *discovery.UnregisterInstanceRequest) (*discovery.UnregisterInstanceResponse, error) {
	remoteIP := util.GetIPFromContext(ctx)
	serviceID := request.ServiceId
	instanceID := request.InstanceId

	instanceFlag := util.StringJoin([]string{serviceID, instanceID}, "/")

	domain := util.ParseDomain(ctx)
	project := util.ParseProject(ctx)
	filter := bson.M{
		model.ColumnDomain:  domain,
		model.ColumnProject: project,
		mutil.StringBuilder([]string{model.ColumnInstance, model.ColumnServiceID}):  serviceID,
		mutil.StringBuilder([]string{model.ColumnInstance, model.ColumnInstanceID}): instanceID}
	result, err := client.GetMongoClient().Delete(ctx, model.CollectionInstance, filter)
	if err != nil || result.DeletedCount == 0 {
		log.Error(fmt.Sprintf("unregister instance failed, instance %s, operator %s revoke instance failed", instanceFlag, remoteIP), err)
		return &discovery.UnregisterInstanceResponse{
			Response: discovery.CreateResponse(discovery.ErrInternal, "delete instance failed"),
		}, err
	}

	log.Info(fmt.Sprintf("unregister instance[%s], operator %s", instanceFlag, remoteIP))
	return &discovery.UnregisterInstanceResponse{
		Response: discovery.CreateResponse(discovery.ResponseSuccess, "Unregister service instance successfully."),
	}, nil
}

func (ds *DataSource) Heartbeat(ctx context.Context, request *discovery.HeartbeatRequest) (*discovery.HeartbeatResponse, error) {
	remoteIP := util.GetIPFromContext(ctx)
	instanceFlag := util.StringJoin([]string{request.ServiceId, request.InstanceId}, "/")
	err := mutil.KeepAliveLease(ctx, request)
	if err != nil {
		log.Error(fmt.Sprintf("heartbeat failed, instance %s operator %s", instanceFlag, remoteIP), err)
		resp := &discovery.HeartbeatResponse{
			Response: discovery.CreateResponseWithSCErr(err),
		}
		if err.InternalError() {
			return resp, err
		}
		return resp, nil
	}
	return &discovery.HeartbeatResponse{
		Response: discovery.CreateResponse(discovery.ResponseSuccess,
			"Update service instance heartbeat successfully."),
	}, nil
}

func (ds *DataSource) HeartbeatSet(ctx context.Context, request *discovery.HeartbeatSetRequest) (*discovery.HeartbeatSetResponse, error) {
	domainProject := util.ParseDomainProject(ctx)

	heartBeatCount := len(request.Instances)
	existFlag := make(map[string]bool, heartBeatCount)
	instancesHbRst := make(chan *discovery.InstanceHbRst, heartBeatCount)
	noMultiCounter := 0

	for _, heartbeatElement := range request.Instances {
		if _, ok := existFlag[heartbeatElement.ServiceId+heartbeatElement.InstanceId]; ok {
			log.Warn(fmt.Sprintf("instance[%s/%s] is duplicate request heartbeat set",
				heartbeatElement.ServiceId, heartbeatElement.InstanceId))
			continue
		} else {
			existFlag[heartbeatElement.ServiceId+heartbeatElement.InstanceId] = true
			noMultiCounter++
		}
		gopool.Go(mutil.GetHeartbeatFunc(ctx, domainProject, instancesHbRst, heartbeatElement))
	}

	count := 0
	successFlag := false
	failFlag := false
	instanceHbRstArr := make([]*discovery.InstanceHbRst, 0, heartBeatCount)

	for hbRst := range instancesHbRst {
		count++
		if len(hbRst.ErrMessage) != 0 {
			failFlag = true
		} else {
			successFlag = true
		}
		instanceHbRstArr = append(instanceHbRstArr, hbRst)
		if count == noMultiCounter {
			close(instancesHbRst)
		}
	}

	if !failFlag && successFlag {
		log.Info(fmt.Sprintf("batch update heartbeats[%d] successfully", count))
		return &discovery.HeartbeatSetResponse{
			Response:  discovery.CreateResponse(discovery.ResponseSuccess, "Heartbeat set successfully."),
			Instances: instanceHbRstArr,
		}, nil
	}

	log.Info(fmt.Sprintf("batch update heartbeats failed %v", request.Instances))
	return &discovery.HeartbeatSetResponse{
		Response:  discovery.CreateResponse(discovery.ErrInstanceNotExists, "Heartbeat set failed."),
		Instances: instanceHbRstArr,
	}, nil
}

func (ds *DataSource) BatchFind(ctx context.Context, request *discovery.BatchFindInstancesRequest) (*discovery.BatchFindInstancesResponse, error) {
	response := &discovery.BatchFindInstancesResponse{
		Response: discovery.CreateResponse(discovery.ResponseSuccess, "Batch query service instances successfully."),
	}

	var err error

	response.Services, err = ds.batchFindServices(ctx, request)
	if err != nil {
		return &discovery.BatchFindInstancesResponse{
			Response: discovery.CreateResponse(discovery.ErrInternal, err.Error()),
		}, err
	}

	response.Instances, err = ds.batchFindInstances(ctx, request)
	if err != nil {
		return &discovery.BatchFindInstancesResponse{
			Response: discovery.CreateResponse(discovery.ErrInternal, err.Error()),
		}, err
	}

	return response, nil
}

func (ds *DataSource) findSharedServiceInstance(ctx context.Context, request *discovery.FindInstancesRequest, provider *discovery.MicroServiceKey, rev string) (*discovery.FindInstancesResponse, error) {
	var err error
	// it means the shared micro-services must be the same env with SC.
	provider.Environment = apt.Service.Environment
	findFlag := func() string {
		return fmt.Sprintf("find shared provider[%s/%s/%s/%s]", provider.Environment, provider.AppId, provider.ServiceName, provider.Version)
	}
	basicFilterServices, err := mutil.ServicesBasicFilter(ctx, provider)
	if err != nil {
		log.Error(fmt.Sprintf("find shared service instance failed %s", findFlag()), err)
		return &discovery.FindInstancesResponse{
			Response: discovery.CreateResponse(discovery.ErrInternal, err.Error()),
		}, err
	}
	services, err := mutil.FindServices(ctx, provider)
	if err != nil {
		log.Error(fmt.Sprintf("find shared service instance failed %s", findFlag()), err)
		return &discovery.FindInstancesResponse{
			Response: discovery.CreateResponse(discovery.ErrInternal, err.Error()),
		}, err
	}
	if services == nil && len(basicFilterServices) == 0 {
		mes := fmt.Errorf("%s failed, provider does not exist", findFlag())
		log.Error("find shared service instance failed", mes)
		return &discovery.FindInstancesResponse{
			Response: discovery.CreateResponse(discovery.ErrServiceNotExists, mes.Error()),
		}, nil
	}
	serviceIDs := mutil.FilterServiceIDs(ctx, request.ConsumerServiceId, request.Tags, services)
	instances, err := mutil.InstancesFilter(ctx, serviceIDs)
	if err != nil {
		log.Error(fmt.Sprintf("find shared service instance failed %s", findFlag()), err)
		return &discovery.FindInstancesResponse{
			Response: discovery.CreateResponse(discovery.ErrInternal, err.Error()),
		}, err
	}
	newRev, _ := mutil.FormatRevision(request.ConsumerServiceId, instances)
	if rev == newRev {
		instances = nil // for gRPC
	}
	// TODO support gRPC output context
	_ = util.WithResponseRev(ctx, newRev)
	return &discovery.FindInstancesResponse{
		Response:  discovery.CreateResponse(discovery.ResponseSuccess, "Query service instances successfully."),
		Instances: instances,
	}, nil
}

func (ds *DataSource) findInstance(ctx context.Context, request *discovery.FindInstancesRequest, provider *discovery.MicroServiceKey, rev string) (*discovery.FindInstancesResponse, error) {
	var err error
	domainProject := util.ParseDomainProject(ctx)
	service := &model.Service{Service: &discovery.MicroService{Environment: request.Environment}}
	if len(request.ConsumerServiceId) > 0 {
		filter := mutil.GeneratorServiceFilter(ctx, request.ConsumerServiceId)
		service, err = mutil.GetService(ctx, filter)
		if err != nil {
			if errors.Is(err, datasource.ErrNoData) {
				log.Debug(fmt.Sprintf("consumer does not exist, consumer %s find provider %s/%s/%s/%s",
					request.ConsumerServiceId, request.Environment, request.AppId, request.ServiceName, request.VersionRule))
				return &discovery.FindInstancesResponse{
					Response: discovery.CreateResponse(discovery.ErrServiceNotExists,
						fmt.Sprintf("Consumer[%s] does not exist.", request.ConsumerServiceId)),
				}, nil
			}
			log.Error(fmt.Sprintf("get consumer failed, consumer %s find provider %s/%s/%s/%s",
				request.ConsumerServiceId, request.Environment, request.AppId, request.ServiceName, request.VersionRule), err)
			return &discovery.FindInstancesResponse{
				Response: discovery.CreateResponse(discovery.ErrInternal, err.Error()),
			}, err
		}
		provider.Environment = service.Service.Environment
	}

	// provider is not a shared micro-service,
	// only allow shared micro-service instances found request different domains.
	ctx = util.SetTargetDomainProject(ctx, util.ParseDomain(ctx), util.ParseProject(ctx))
	provider.Tenant = util.ParseTargetDomainProject(ctx)

	findFlag := func() string {
		return fmt.Sprintf("Consumer[%s][%s/%s/%s/%s] find provider[%s/%s/%s/%s]",
			request.ConsumerServiceId, service.Service.Environment, service.Service.AppId, service.Service.ServiceName, service.Service.Version,
			provider.Environment, provider.AppId, provider.ServiceName, provider.Version)
	}
	basicFilterServices, err := mutil.ServicesBasicFilter(ctx, provider)
	if err != nil {
		log.Error(fmt.Sprintf("find instance failed %s", findFlag()), err)
		return &discovery.FindInstancesResponse{
			Response: discovery.CreateResponse(discovery.ErrInternal, err.Error()),
		}, err
	}
	services, err := mutil.FindServices(ctx, provider)
	if err != nil {
		log.Error(fmt.Sprintf("find instance failed %s", findFlag()), err)
		return &discovery.FindInstancesResponse{
			Response: discovery.CreateResponse(discovery.ErrInternal, err.Error()),
		}, err
	}
	if services == nil && len(basicFilterServices) == 0 {
		mes := fmt.Errorf("%s failed, provider does not exist", findFlag())
		log.Error("find instance failed", mes)
		return &discovery.FindInstancesResponse{
			Response: discovery.CreateResponse(discovery.ErrServiceNotExists, mes.Error()),
		}, nil
	}
	serviceIDs := mutil.FilterServiceIDs(ctx, request.ConsumerServiceId, request.Tags, services)
	instances, err := mutil.InstancesFilter(ctx, serviceIDs)
	if err != nil {
		log.Error(fmt.Sprintf("find instance failed %s", findFlag()), err)
		return &discovery.FindInstancesResponse{
			Response: discovery.CreateResponse(discovery.ErrInternal, err.Error()),
		}, err
	}
	// add dependency queue
	if len(request.ConsumerServiceId) > 0 &&
		len(serviceIDs) > 0 {
		provider, err = ds.reshapeProviderKey(ctx, provider, serviceIDs[0])
		if err != nil {
			return nil, err
		}
		if provider != nil {
			err = mutil.AddServiceVersionRule(ctx, domainProject, service.Service, provider)
		} else {
			mes := fmt.Errorf("%s failed, provider does not exist", findFlag())
			log.Error("add service version rule failed", mes)
			return &discovery.FindInstancesResponse{
				Response: discovery.CreateResponse(discovery.ErrServiceNotExists, mes.Error()),
			}, nil
		}
		if err != nil {
			log.Error(fmt.Sprintf("add service version rule failed %s", findFlag()), err)
			return &discovery.FindInstancesResponse{
				Response: discovery.CreateResponse(discovery.ErrInternal, err.Error()),
			}, err
		}
	}
	newRev, _ := mutil.FormatRevision(request.ConsumerServiceId, instances)
	if rev == newRev {
		instances = nil // for gRPC
	}
	// TODO support gRPC output context
	_ = util.WithResponseRev(ctx, newRev)
	return &discovery.FindInstancesResponse{
		Response:  discovery.CreateResponse(discovery.ResponseSuccess, "Query service instances successfully."),
		Instances: instances,
	}, nil
}

func (ds *DataSource) reshapeProviderKey(ctx context.Context, provider *discovery.MicroServiceKey, providerID string) (*discovery.MicroServiceKey, error) {
	//维护version的规则,service name 可能是别名，所以重新获取
	filter := mutil.GeneratorServiceFilter(ctx, providerID)
	providerService, err := mutil.GetService(ctx, filter)
	if err != nil {
		return nil, err
	}

	versionRule := provider.Version
	provider = discovery.MicroServiceToKey(provider.Tenant, providerService.Service)
	provider.Version = versionRule
	return provider, nil
}

func (ds *DataSource) batchFindServices(ctx context.Context, request *discovery.BatchFindInstancesRequest) (*discovery.BatchFindResult, error) {
	if len(request.Services) == 0 {
		return nil, nil
	}
	cloneCtx := util.CloneContext(ctx)

	services := &discovery.BatchFindResult{}
	failedResult := make(map[int32]*discovery.FindFailedResult)
	for index, key := range request.Services {
		findCtx := util.SetContext(cloneCtx, util.CtxRequestRevision, key.Rev)
		resp, err := ds.FindInstances(findCtx, &discovery.FindInstancesRequest{
			ConsumerServiceId: request.ConsumerServiceId,
			AppId:             key.Service.AppId,
			ServiceName:       key.Service.ServiceName,
			VersionRule:       key.Service.Version,
			Environment:       key.Service.Environment,
		})
		if err != nil {
			return nil, err
		}
		failed, ok := failedResult[resp.Response.GetCode()]
		mutil.AppendFindResponse(findCtx, int64(index), resp.Response, resp.Instances,
			&services.Updated, &services.NotModified, &failed)
		if !ok && failed != nil {
			failedResult[resp.Response.GetCode()] = failed
		}
	}
	for _, result := range failedResult {
		services.Failed = append(services.Failed, result)
	}
	return services, nil
}

func (ds *DataSource) batchFindInstances(ctx context.Context, request *discovery.BatchFindInstancesRequest) (*discovery.BatchFindResult, error) {
	if len(request.Instances) == 0 {
		return nil, nil
	}
	cloneCtx := util.CloneContext(ctx)
	// can not find the shared provider instances
	cloneCtx = util.SetTargetDomainProject(cloneCtx, util.ParseDomain(ctx), util.ParseProject(ctx))

	instances := &discovery.BatchFindResult{}
	failedResult := make(map[int32]*discovery.FindFailedResult)
	for index, key := range request.Instances {
		getCtx := util.SetContext(cloneCtx, util.CtxRequestRevision, key.Rev)
		resp, err := ds.GetInstance(getCtx, &discovery.GetOneInstanceRequest{
			ConsumerServiceId:  request.ConsumerServiceId,
			ProviderServiceId:  key.Instance.ServiceId,
			ProviderInstanceId: key.Instance.InstanceId,
		})
		if err != nil {
			return nil, err
		}
		failed, ok := failedResult[resp.Response.GetCode()]
		mutil.AppendFindResponse(getCtx, int64(index), resp.Response, []*discovery.MicroServiceInstance{resp.Instance},
			&instances.Updated, &instances.NotModified, &failed)
		if !ok && failed != nil {
			failedResult[resp.Response.GetCode()] = failed
		}
	}
	for _, result := range failedResult {
		instances.Failed = append(instances.Failed, result)
	}
	return instances, nil
}
