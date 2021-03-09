package dao

import (
	"context"

	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/apache/servicecomb-service-center/datasource/mongo/client"
	"github.com/apache/servicecomb-service-center/datasource/mongo/model"
	"github.com/apache/servicecomb-service-center/pkg/log"
)

func GetRule(ctx context.Context, filter interface{}, opts ...*options.FindOneOptions) (*model.Rule, error) {
	result, err := client.GetMongoClient().FindOne(ctx, model.CollectionRule, filter, opts...)
	if err != nil {
		return nil, err
	}
	if result.Err() != nil {
		log.Error("fail to get rule", result.Err())
		return nil, model.ErrNoData
	}
	var rule *model.Rule
	err = result.Decode(&rule)
	if err != nil {
		log.Error("type conversion error", err)
		return nil, err
	}
	return rule, nil
}

func GetRules(ctx context.Context, filter interface{}, opts ...*options.FindOptions) ([]*model.Rule, error) {
	cursor, err := client.GetMongoClient().Find(ctx, model.CollectionRule, filter, opts...)
	if err != nil {
		return nil, err
	}
	if cursor.Err() != nil {
		log.Error("fail to get rules", cursor.Err())
		return nil, cursor.Err()
	}
	var rules []*model.Rule
	defer cursor.Close(ctx)
	for cursor.Next(ctx) {
		var rule model.Rule
		err := cursor.Decode(&rule)
		if err != nil {
			log.Error("type conversion error", err)
			return nil, err
		}
		rules = append(rules, &rule)
	}
	return rules, nil
}

