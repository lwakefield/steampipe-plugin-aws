package aws

import (
	"context"
	"errors"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/wellarchitected"
	"github.com/aws/aws-sdk-go-v2/service/wellarchitected/types"
	"github.com/aws/smithy-go"

	wellarchitectedv1 "github.com/aws/aws-sdk-go/service/wellarchitected"

	"github.com/turbot/go-kit/helpers"
	"github.com/turbot/steampipe-plugin-sdk/v5/grpc/proto"
	"github.com/turbot/steampipe-plugin-sdk/v5/plugin"
	"github.com/turbot/steampipe-plugin-sdk/v5/plugin/transform"
)

//// TABLE DEFINITION

func tableAwsWellArchitectedLens(_ context.Context) *plugin.Table {
	return &plugin.Table{
		Name:        "aws_wellarchitected_lens",
		Description: "AWS Well-Architected Lens",
		List: &plugin.ListConfig{
			Hydrate: listWellArchitectedLenses,
			IgnoreConfig: &plugin.IgnoreConfig{
				ShouldIgnoreErrorFunc: shouldIgnoreErrors([]string{"ValidationException"}),
			},
			KeyColumns: []*plugin.KeyColumn{
				{Name: "lens_name", Require: plugin.Optional},
				{Name: "lens_status", Require: plugin.Optional},
				{Name: "lens_type", Require: plugin.Optional},
			},
		},
		GetMatrixItemFunc: SupportedRegionMatrix(wellarchitectedv1.EndpointsID),
		Columns: awsRegionalColumns([]*plugin.Column{
			{
				Name:        "lens_name",
				Description: "The full name of the lens.",
				Type:        proto.ColumnType_STRING,
			},
			{
				Name:        "lens_alias",
				Description: "The alias of the lens.",
				Type:        proto.ColumnType_STRING,
			},
			{
				Name:        "arn",
				Description: "The Amazon Resource Name (ARN) of the lens.",
				Type:        proto.ColumnType_STRING,
				Transform:   transform.FromField("LensArn"),
			},
			{
				Name:        "created_at",
				Description: "The date and time when the lens was created.",
				Type:        proto.ColumnType_TIMESTAMP,
			},
			{
				Name:        "updated_at",
				Description: "The date and time when the lens was last updated.",
				Type:        proto.ColumnType_TIMESTAMP,
			},
			{
				Name:        "description",
				Description: "The description of the lens.",
				Type:        proto.ColumnType_STRING,
			},
			{
				Name:        "lens_status",
				Description: "The status of the lens.",
				Type:        proto.ColumnType_STRING,
			},
			{
				Name:        "lens_type",
				Description: "The type of the lens.",
				Type:        proto.ColumnType_STRING,
			},
			{
				Name:        "lens_version",
				Description: "The version of the lens.",
				Type:        proto.ColumnType_STRING,
			},
			{
				Name:        "owner",
				Description: "An Amazon Web Services account ID.",
				Type:        proto.ColumnType_STRING,
			},
			{
				Name:        "share_invitation_id",
				Description: "The ID assigned to the shared invitation.",
				Type:        proto.ColumnType_STRING,
				Hydrate:     getWellArchitectedLens,
				Transform:   transform.FromField("ShareInvitationId"),
			},

			// Steampipe standard columns
			{
				Name:        "title",
				Description: resourceInterfaceDescription("title"),
				Type:        proto.ColumnType_STRING,
				Transform:   transform.FromField("LensName"),
			},
			{
				Name:        "tags",
				Description: resourceInterfaceDescription("tags"),
				Type:        proto.ColumnType_JSON,
				Hydrate:     getWellArchitectedLens,
				Transform:   transform.FromField("Tags"),
			},
			{
				Name:        "akas",
				Description: resourceInterfaceDescription("akas"),
				Type:        proto.ColumnType_JSON,
				Transform:   transform.FromField("LensArn").Transform(transform.EnsureStringArray),
			},
		}),
	}
}

//// LIST FUNCTION

func listWellArchitectedLenses(ctx context.Context, d *plugin.QueryData, _ *plugin.HydrateData) (interface{}, error) {
	// Create session
	svc, err := WellArchitectedClient(ctx, d)
	if err != nil {
		plugin.Logger(ctx).Error("aws_wellarchitected_lens.listWellArchitectedLenses", "connection_error", err)
		return nil, err
	}
	if svc == nil {
		// Unsupported region, return no data
		return nil, nil
	}

	// Limiting the results
	maxLimit := int32(50)
	if d.QueryContext.Limit != nil {
		limit := int32(*d.QueryContext.Limit)
		if limit < maxLimit {
			maxLimit = limit
		}
	}

	input := &wellarchitected.ListLensesInput{
		MaxResults: maxLimit,
	}

	if d.EqualsQualString("lens_status") != "" {
		input.LensStatus = types.LensStatusType(d.EqualsQualString("lens_status"))
	}
	if d.EqualsQualString("lens_name") != "" {
		input.LensName = aws.String(d.EqualsQualString("lens_name"))
	}
	if d.EqualsQualString("lens_type") != "" {
		input.LensType = types.LensType(d.EqualsQualString("lens_type"))
	}

	paginator := wellarchitected.NewListLensesPaginator(svc, input, func(o *wellarchitected.ListLensesPaginatorOptions) {
		o.Limit = maxLimit
		o.StopOnDuplicateToken = true
	})

	// List call
	for paginator.HasMorePages() {
		output, err := paginator.NextPage(ctx)
		if err != nil {
			plugin.Logger(ctx).Error("aws_wellarchitected_lens.listWellArchitectedLenses", "api_error", err)
			return nil, err
		}

		for _, item := range output.LensSummaries {
			// As per the doc(https://docs.aws.amazon.com/wellarchitected/latest/APIReference/API_LensSummary.html)
			// The lens alias is same as lens arn if it is a custom lens.
			// The lens alias value coming as nil in the case of custom lenses, so we are replacing the null value with lens arn.
			if item.LensAlias == nil || *item.LensAlias == "" {
				item.LensAlias = item.LensArn
			}

			d.StreamListItem(ctx, item)

			// Context can be cancelled due to manual cancellation or the limit has been hit
			if d.RowsRemaining(ctx) == 0 {
				return nil, nil
			}
		}
	}

	return nil, nil
}

//// HYDRATE FUNCTIONS

// We can have two type of lenses (1. AWS official lenses, 2. Custom lenses)
/*
	AWS official lenses: arn:aws:wellarchitected:us-east-1::lens/serverless
	Custom lenses: arn:aws:wellarchitected:us-west-2:123456789012:lens/0123456789abcdef01234567890abcdef

	The API GetLens(https://docs.aws.amazon.com/wellarchitected/latest/APIReference/API_GetLens.html) can take two parameter.
	1. LensAlias:
		For AWS official lenses, this is either the lens alias, such as serverless, or the lens ARN, such as arn:aws:wellarchitected:us-east-1::lens/serverless
		For custom lenses, this is the lens ARN, such as arn:aws:wellarchitected:us-west-2:123456789012:lens/0123456789abcdef01234567890abcdef
	2. LensVersion:
		The lens version to be retrieved.

	For manipulation the 'LensAlias' values for for both type(AWS official/custom) of lenses is bit wired, also the steampipe query throws error(get call returned 18 results - the key column is not globally unique) in the case of AWS official lenses because the lens aliases are same throughout the regions, so this function can not be use in get config.

	This function has been added as a hydrate function to overcome the error "get call returned 18 results - the key column is not globally unique"
*/

func getWellArchitectedLens(ctx context.Context, d *plugin.QueryData, h *plugin.HydrateData) (interface{}, error) {

	lensArn := *h.Item.(types.LensSummary).LensArn

	// Create Session
	svc, err := WellArchitectedClient(ctx, d)
	if err != nil {
		plugin.Logger(ctx).Error("aws_wellarchitected_lens.getWellArchitectedLens", "connection_error", err)
		return nil, err
	}

	if svc == nil {
		// Unsupported region, return no data
		return nil, nil
	}

	params := &wellarchitected.GetLensInput{
		LensAlias: aws.String(lensArn),
	}

	op, err := svc.GetLens(ctx, params)
	if err != nil {
		var ae smithy.APIError
		if errors.As(err, &ae) {
			if helpers.StringSliceContains([]string{"ResourceNotFoundException"}, ae.ErrorCode()) {
				return nil, nil
			}
		}
		plugin.Logger(ctx).Error("aws_wellarchitected_lens.getWellArchitectedLens", "api_error", err)
		return nil, err
	}
	return op.Lens, nil
}