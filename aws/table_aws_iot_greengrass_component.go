package aws

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/greengrassv2"
	"github.com/aws/aws-sdk-go-v2/service/greengrassv2/types"

	greengrassv1 "github.com/aws/aws-sdk-go/service/greengrassv2"

	"github.com/turbot/steampipe-plugin-sdk/v5/grpc/proto"
	"github.com/turbot/steampipe-plugin-sdk/v5/plugin"
	"github.com/turbot/steampipe-plugin-sdk/v5/plugin/transform"
)

//// TABLE DEFINITION

func tableAwsIotGreengrassComponent(_ context.Context) *plugin.Table {
	return &plugin.Table{
		Name:        "aws_iot_greengrass_component",
		Description: "AWS IoT Greengrass Component",
		List: &plugin.ListConfig{
			Hydrate: listIoTGreengrassComponents,
			Tags:    map[string]string{"service": "greengrassv2", "action": "ListComponents"},
		},
		// The `get config` is not included here for several reasons:
		//   1. The API only returns the recipe and recipeOutputFormat, without any additional information.
		//   2. A specific version is required to make the API call, but we obtain the latest version through the list call.
		//   3. Utilizing the GetComponent API as a hydrated call is a more effective approach.
		HydrateConfig: []plugin.HydrateConfig{
			{
				Func: getIoTGreengrassComponent,
				Tags: map[string]string{"service": "greengrassv2", "action": "GetComponent"},
				IgnoreConfig: &plugin.IgnoreConfig{
					ShouldIgnoreErrorFunc: shouldIgnoreErrors([]string{"ResourceNotFoundException"}),
				},
			},
		},
		GetMatrixItemFunc: SupportedRegionMatrix(greengrassv1.EndpointsID),
		Columns: awsRegionalColumns([]*plugin.Column{
			{
				Name:        "component_name",
				Description: "The name of the component.",
				Type:        proto.ColumnType_STRING,
			},
			{
				Name:        "arn",
				Description: "The ARN of the component.",
				Type:        proto.ColumnType_STRING,
			},
			{
				Name:        "component_version_arn",
				Description: "The ARN of the component version.",
				Type:        proto.ColumnType_STRING,
				Transform:   transform.FromField("LatestVersion.Arn"),
			},
			{
				Name:        "component_version",
				Description: "The version of the component.",
				Type:        proto.ColumnType_STRING,
				Transform:   transform.FromField("LatestVersion.ComponentVersion"),
			},
			{
				Name:        "recipe",
				Description: "The recipe of the component version.",
				Type:        proto.ColumnType_STRING,
				Hydrate:     getIoTGreengrassComponent,
			},
			{
				Name:        "recipe_output_format",
				Description: "The format of the recipe.",
				Type:        proto.ColumnType_STRING,
				Hydrate:     getIoTGreengrassComponent,
			},

			// JSON columns
			{
				Name:        "latest_version",
				Description: "The latest version of the component and its details.",
				Type:        proto.ColumnType_JSON,
			},

			// Steampipe standard columns
			{
				Name:        "title",
				Description: resourceInterfaceDescription("title"),
				Type:        proto.ColumnType_STRING,
				Transform:   transform.FromField("ComponentName"),
			},
			{
				Name:        "tags",
				Description: resourceInterfaceDescription("tags"),
				Type:        proto.ColumnType_JSON,
				Hydrate:     getIoTGreengrassComponent,
			},
			{
				Name:        "akas",
				Description: resourceInterfaceDescription("akas"),
				Type:        proto.ColumnType_JSON,
				Transform:   transform.FromField("Arn").Transform(transform.EnsureStringArray),
			},
		}),
	}
}

//// LIST FUNCTION

func listIoTGreengrassComponents(ctx context.Context, d *plugin.QueryData, _ *plugin.HydrateData) (interface{}, error) {
	// Create Session
	svc, err := IoTGreengrassClient(ctx, d)
	if err != nil {
		plugin.Logger(ctx).Error("aws_iot_greengrass_component.listIoTGreengrassComponents", "connection_error", err)
		return nil, err
	}
	if svc == nil {
		// Unsupported region, return no data
		return nil, nil
	}

	// Limiting the results
	maxLimit := int32(250)
	if d.QueryContext.Limit != nil {
		limit := int32(*d.QueryContext.Limit)
		if limit < maxLimit {
			maxLimit = limit
		}
	}

	input := &greengrassv2.ListComponentsInput{
		MaxResults: aws.Int32(maxLimit),
	}

	paginator := greengrassv2.NewListComponentsPaginator(svc, input, func(o *greengrassv2.ListComponentsPaginatorOptions) {
		o.Limit = maxLimit
		o.StopOnDuplicateToken = true
	})

	// List call
	for paginator.HasMorePages() {
		// apply rate limiting
		d.WaitForListRateLimit(ctx)

		output, err := paginator.NextPage(ctx)
		if err != nil {
			plugin.Logger(ctx).Error("aws_iot_greengrass_component.listIoTGreengrassComponents", "api_error", err)
			return nil, err
		}

		for _, item := range output.Components {
			d.StreamListItem(ctx, item)

			// Context can be cancelled due to manual cancellation or the limit has been hit
			if d.RowsRemaining(ctx) == 0 {
				return nil, nil
			}
		}
	}

	return nil, err
}

//// HYDRATE FUNCTIONS

func getIoTGreengrassComponent(ctx context.Context, d *plugin.QueryData, h *plugin.HydrateData) (interface{}, error) {
	arn := ""
	componentVersion := ""
	if h.Item != nil {
		item := h.Item.(types.Component)
		arn = *item.Arn
		componentVersion = *item.LatestVersion.ComponentVersion
	}

	// Empty check
	if arn == "" || componentVersion == "" {
		return nil, nil
	}

	// Create service
	svc, err := IoTGreengrassClient(ctx, d)
	if err != nil {
		plugin.Logger(ctx).Error("aws_iot_greengrass_component.getIoTGreengrassComponent", "connection_error", err)
		return nil, err
	}

	params := &greengrassv2.GetComponentInput{
		Arn: aws.String(arn + ":versions:" + componentVersion),
	}

	resp, err := svc.GetComponent(ctx, params)
	if err != nil {
		plugin.Logger(ctx).Error("aws_iot_greengrass_component.getIoTGreengrassComponent", "api_error", err)
		return nil, err
	}

	return resp, nil
}