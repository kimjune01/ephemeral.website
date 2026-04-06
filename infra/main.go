package main

import (
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/acm"
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/apigatewayv2"
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/dynamodb"
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/iam"
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/lambda"
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/route53"
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/s3"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

const domain = "ephemeral.website"

func main() {
	pulumi.Run(func(ctx *pulumi.Context) error {
		// ── S3: audio storage ──
		audioBucket, err := s3.NewBucket(ctx, "ephemeral-audio", &s3.BucketArgs{
			ForceDestroy: pulumi.Bool(true),
			CorsRules: s3.BucketCorsRuleArray{
				&s3.BucketCorsRuleArgs{
					AllowedHeaders: pulumi.StringArray{pulumi.String("*")},
					AllowedMethods: pulumi.StringArray{pulumi.String("PUT")},
					AllowedOrigins: pulumi.StringArray{pulumi.String("*")},
					MaxAgeSeconds:  pulumi.Int(3600),
				},
			},
			LifecycleRules: s3.BucketLifecycleRuleArray{
				&s3.BucketLifecycleRuleArgs{
					Enabled: pulumi.Bool(true),
					Prefix:  pulumi.String("audio/"),
					Expiration: &s3.BucketLifecycleRuleExpirationArgs{
						Days: pulumi.Int(2),
					},
				},
			},
		})
		if err != nil {
			return err
		}

		// ── DynamoDB ──
		tokensTable, err := dynamodb.NewTable(ctx, "ephemeral-tokens", &dynamodb.TableArgs{
			BillingMode: pulumi.String("PAY_PER_REQUEST"),
			HashKey:     pulumi.String("token"),
			Attributes: dynamodb.TableAttributeArray{
				&dynamodb.TableAttributeArgs{
					Name: pulumi.String("token"),
					Type: pulumi.String("S"),
				},
			},
			Ttl: &dynamodb.TableTtlArgs{
				AttributeName: pulumi.String("ttl"),
				Enabled:       pulumi.Bool(true),
			},
		})
		if err != nil {
			return err
		}

		sessionsTable, err := dynamodb.NewTable(ctx, "ephemeral-sessions", &dynamodb.TableArgs{
			BillingMode: pulumi.String("PAY_PER_REQUEST"),
			HashKey:     pulumi.String("session_id"),
			Attributes: dynamodb.TableAttributeArray{
				&dynamodb.TableAttributeArgs{
					Name: pulumi.String("session_id"),
					Type: pulumi.String("S"),
				},
			},
			Ttl: &dynamodb.TableTtlArgs{
				AttributeName: pulumi.String("ttl"),
				Enabled:       pulumi.Bool(true),
			},
		})
		if err != nil {
			return err
		}

		// ── IAM ──
		lambdaRole, err := iam.NewRole(ctx, "ephemeral-lambda-role", &iam.RoleArgs{
			AssumeRolePolicy: pulumi.String(`{
				"Version": "2012-10-17",
				"Statement": [{
					"Action": "sts:AssumeRole",
					"Principal": {"Service": "lambda.amazonaws.com"},
					"Effect": "Allow"
				}]
			}`),
		})
		if err != nil {
			return err
		}

		_, err = iam.NewRolePolicyAttachment(ctx, "lambda-basic", &iam.RolePolicyAttachmentArgs{
			Role:      lambdaRole.Name,
			PolicyArn: pulumi.String("arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"),
		})
		if err != nil {
			return err
		}

		_, err = iam.NewRolePolicy(ctx, "lambda-app-policy", &iam.RolePolicyArgs{
			Role: lambdaRole.ID(),
			Policy: pulumi.All(audioBucket.Arn, tokensTable.Arn, sessionsTable.Arn).ApplyT(
				func(args []interface{}) string {
					bucketArn := args[0].(string)
					tokensArn := args[1].(string)
					sessionsArn := args[2].(string)
					return `{
						"Version": "2012-10-17",
						"Statement": [
							{
								"Effect": "Allow",
								"Action": ["s3:GetObject", "s3:PutObject", "s3:DeleteObject"],
								"Resource": "` + bucketArn + `/*"
							},
							{
								"Effect": "Allow",
								"Action": ["dynamodb:GetItem", "dynamodb:PutItem", "dynamodb:DeleteItem", "dynamodb:UpdateItem"],
								"Resource": ["` + tokensArn + `", "` + sessionsArn + `"]
							}
						]
					}`
				},
			).(pulumi.StringOutput),
		})
		if err != nil {
			return err
		}

		// ── Lambda functions ──
		lambdaEnv := &lambda.FunctionEnvironmentArgs{
			Variables: pulumi.StringMap{
				"AUDIO_BUCKET":   audioBucket.ID(),
				"TOKENS_TABLE":   tokensTable.Name,
				"SESSIONS_TABLE": sessionsTable.Name,
				"PAUSE_TIMEOUT":  pulumi.String("15"),
			},
		}

		goRuntime := pulumi.String("provided.al2023")
		goArch := pulumi.String("arm64")
		goHandler := pulumi.String("bootstrap")

		uploadFn, err := lambda.NewFunction(ctx, "ephemeral-upload", &lambda.FunctionArgs{
			Runtime: goRuntime, Handler: goHandler, Architectures: pulumi.StringArray{goArch},
			Role: lambdaRole.Arn, Code: pulumi.NewFileArchive("../backend/dist/upload"),
			Environment: lambdaEnv, Timeout: pulumi.Int(10), MemorySize: pulumi.Int(128),
		})
		if err != nil {
			return err
		}

		sessionFn, err := lambda.NewFunction(ctx, "ephemeral-session", &lambda.FunctionArgs{
			Runtime: goRuntime, Handler: goHandler, Architectures: pulumi.StringArray{goArch},
			Role: lambdaRole.Arn, Code: pulumi.NewFileArchive("../backend/dist/session"),
			Environment: lambdaEnv, Timeout: pulumi.Int(10), MemorySize: pulumi.Int(128),
		})
		if err != nil {
			return err
		}

		streamFn, err := lambda.NewFunction(ctx, "ephemeral-stream", &lambda.FunctionArgs{
			Runtime: goRuntime, Handler: goHandler, Architectures: pulumi.StringArray{goArch},
			Role: lambdaRole.Arn, Code: pulumi.NewFileArchive("../backend/dist/stream"),
			Environment: lambdaEnv, Timeout: pulumi.Int(10), MemorySize: pulumi.Int(128),
		})
		if err != nil {
			return err
		}

		heartbeatFn, err := lambda.NewFunction(ctx, "ephemeral-heartbeat", &lambda.FunctionArgs{
			Runtime: goRuntime, Handler: goHandler, Architectures: pulumi.StringArray{goArch},
			Role: lambdaRole.Arn, Code: pulumi.NewFileArchive("../backend/dist/heartbeat"),
			Environment: lambdaEnv, Timeout: pulumi.Int(5), MemorySize: pulumi.Int(128),
		})
		if err != nil {
			return err
		}

		completeFn, err := lambda.NewFunction(ctx, "ephemeral-complete", &lambda.FunctionArgs{
			Runtime: goRuntime, Handler: goHandler, Architectures: pulumi.StringArray{goArch},
			Role: lambdaRole.Arn, Code: pulumi.NewFileArchive("../backend/dist/complete"),
			Environment: lambdaEnv, Timeout: pulumi.Int(10), MemorySize: pulumi.Int(128),
		})
		if err != nil {
			return err
		}

		checkFn, err := lambda.NewFunction(ctx, "ephemeral-check", &lambda.FunctionArgs{
			Runtime: goRuntime, Handler: goHandler, Architectures: pulumi.StringArray{goArch},
			Role: lambdaRole.Arn, Code: pulumi.NewFileArchive("../backend/dist/check"),
			Environment: lambdaEnv, Timeout: pulumi.Int(5), MemorySize: pulumi.Int(128),
		})
		if err != nil {
			return err
		}

		siteFn, err := lambda.NewFunction(ctx, "ephemeral-site", &lambda.FunctionArgs{
			Runtime: goRuntime, Handler: goHandler, Architectures: pulumi.StringArray{goArch},
			Role: lambdaRole.Arn, Code: pulumi.NewFileArchive("../backend/dist/site"),
			Timeout: pulumi.Int(5), MemorySize: pulumi.Int(128),
		})
		if err != nil {
			return err
		}

		// ── ACM certificate ──
		cert, err := acm.NewCertificate(ctx, "ephemeral-cert", &acm.CertificateArgs{
			DomainName:       pulumi.String(domain),
			ValidationMethod: pulumi.String("DNS"),
		})
		if err != nil {
			return err
		}

		// ── Route53 hosted zone ──
		zone, err := route53.NewZone(ctx, "ephemeral-zone", &route53.ZoneArgs{
			Name: pulumi.String(domain),
		})
		if err != nil {
			return err
		}

		validationRecord, err := route53.NewRecord(ctx, "ephemeral-cert-validation", &route53.RecordArgs{
			ZoneId: zone.ZoneId,
			Name:   cert.DomainValidationOptions.Index(pulumi.Int(0)).ResourceRecordName().Elem(),
			Type:   cert.DomainValidationOptions.Index(pulumi.Int(0)).ResourceRecordType().Elem(),
			Records: pulumi.StringArray{
				cert.DomainValidationOptions.Index(pulumi.Int(0)).ResourceRecordValue().Elem(),
			},
			Ttl: pulumi.Int(60),
		})
		if err != nil {
			return err
		}

		certValidation, err := acm.NewCertificateValidation(ctx, "ephemeral-cert-valid", &acm.CertificateValidationArgs{
			CertificateArn:        cert.Arn,
			ValidationRecordFqdns: pulumi.StringArray{validationRecord.Fqdn},
		})
		if err != nil {
			return err
		}

		// ── API Gateway ──
		api, err := apigatewayv2.NewApi(ctx, "ephemeral-api", &apigatewayv2.ApiArgs{
			ProtocolType: pulumi.String("HTTP"),
		})
		if err != nil {
			return err
		}

		_, err = apigatewayv2.NewStage(ctx, "ephemeral-stage", &apigatewayv2.StageArgs{
			ApiId:      api.ID(),
			Name:       pulumi.String("$default"),
			AutoDeploy: pulumi.Bool(true),
		})
		if err != nil {
			return err
		}

		// Custom domain
		domainName, err := apigatewayv2.NewDomainName(ctx, "ephemeral-domain", &apigatewayv2.DomainNameArgs{
			DomainName: pulumi.String(domain),
			DomainNameConfiguration: &apigatewayv2.DomainNameDomainNameConfigurationArgs{
				CertificateArn: certValidation.CertificateArn,
				EndpointType:   pulumi.String("REGIONAL"),
				SecurityPolicy: pulumi.String("TLS_1_2"),
			},
		})
		if err != nil {
			return err
		}

		_, err = apigatewayv2.NewApiMapping(ctx, "ephemeral-mapping", &apigatewayv2.ApiMappingArgs{
			ApiId:      api.ID(),
			DomainName: domainName.ID(),
			Stage:      pulumi.String("$default"),
		})
		if err != nil {
			return err
		}

		// Route53 → API Gateway custom domain
		_, err = route53.NewRecord(ctx, "ephemeral-dns", &route53.RecordArgs{
			ZoneId: zone.ZoneId,
			Name:   pulumi.String(domain),
			Type:   pulumi.String("A"),
			Aliases: route53.RecordAliasArray{
				&route53.RecordAliasArgs{
					Name:                 domainName.DomainNameConfiguration.TargetDomainName().Elem(),
					ZoneId:               domainName.DomainNameConfiguration.HostedZoneId().Elem(),
					EvaluateTargetHealth: pulumi.Bool(false),
				},
			},
		})
		if err != nil {
			return err
		}

		// ── Routes ──
		// API routes
		apiRoutes := []struct {
			name   string
			method string
			path   string
			fn     *lambda.Function
		}{
			{"upload", "POST", "/api/upload", uploadFn},
			{"session", "POST", "/api/session", sessionFn},
			{"stream", "GET", "/api/stream/{session_id}", streamFn},
			{"heartbeat", "POST", "/api/heartbeat/{session_id}", heartbeatFn},
			{"complete", "POST", "/api/complete/{session_id}", completeFn},
			{"check", "GET", "/api/check/{token}", checkFn},
		}

		for _, r := range apiRoutes {
			integration, err := apigatewayv2.NewIntegration(ctx, "integration-"+r.name, &apigatewayv2.IntegrationArgs{
				ApiId:                api.ID(),
				IntegrationType:      pulumi.String("AWS_PROXY"),
				IntegrationUri:       r.fn.Arn,
				PayloadFormatVersion: pulumi.String("2.0"),
			})
			if err != nil {
				return err
			}

			_, err = apigatewayv2.NewRoute(ctx, "route-"+r.name, &apigatewayv2.RouteArgs{
				ApiId:    api.ID(),
				RouteKey: pulumi.Sprintf("%s %s", r.method, r.path),
				Target:   integration.ID().ApplyT(func(id string) string { return "integrations/" + id }).(pulumi.StringOutput),
			})
			if err != nil {
				return err
			}

			_, err = lambda.NewPermission(ctx, "permission-"+r.name, &lambda.PermissionArgs{
				Action:    pulumi.String("lambda:InvokeFunction"),
				Function:  r.fn.Name,
				Principal: pulumi.String("apigateway.amazonaws.com"),
				SourceArn: api.ExecutionArn.ApplyT(func(arn string) string { return arn + "/*/*" }).(pulumi.StringOutput),
			})
			if err != nil {
				return err
			}
		}

		// Site catch-all (lowest priority — $default route)
		siteIntegration, err := apigatewayv2.NewIntegration(ctx, "integration-site", &apigatewayv2.IntegrationArgs{
			ApiId:                api.ID(),
			IntegrationType:      pulumi.String("AWS_PROXY"),
			IntegrationUri:       siteFn.Arn,
			PayloadFormatVersion: pulumi.String("2.0"),
		})
		if err != nil {
			return err
		}

		_, err = apigatewayv2.NewRoute(ctx, "route-site", &apigatewayv2.RouteArgs{
			ApiId:    api.ID(),
			RouteKey: pulumi.String("$default"),
			Target:   siteIntegration.ID().ApplyT(func(id string) string { return "integrations/" + id }).(pulumi.StringOutput),
		})
		if err != nil {
			return err
		}

		_, err = lambda.NewPermission(ctx, "permission-site", &lambda.PermissionArgs{
			Action:    pulumi.String("lambda:InvokeFunction"),
			Function:  siteFn.Name,
			Principal: pulumi.String("apigateway.amazonaws.com"),
			SourceArn: api.ExecutionArn.ApplyT(func(arn string) string { return arn + "/*/*" }).(pulumi.StringOutput),
		})
		if err != nil {
			return err
		}

		// ── Exports ──
		ctx.Export("apiUrl", api.ApiEndpoint)
		ctx.Export("audioBucket", audioBucket.ID())
		ctx.Export("nameServers", zone.NameServers)
		ctx.Export("tokensTable", tokensTable.Name)
		ctx.Export("sessionsTable", sessionsTable.Name)

		return nil
	})
}
