package rbac

import (
	"testing"

	carbonpanelv1connect "github.com/athNdev/carbon-panel/pkg/proto/carbonpanel/v1/carbonpanelv1connect"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
)

// procedureServiceNames lists every carbonpanel.v1 service that is registered
// as a Connect handler in internal/rpc/server.go. It is the source of truth
// used to reconstruct the full set of RPC procedure paths via the protobuf
// service descriptors, so a newly added service (or a service whose descriptor
// failed to register) surfaces here rather than silently passing.
var procedureServiceNames = []string{
	carbonpanelv1connect.AuthServiceName,
	carbonpanelv1connect.ConfigServiceName,
	carbonpanelv1connect.FileServiceName,
	carbonpanelv1connect.MinecraftServiceName,
	carbonpanelv1connect.ModServiceName,
	carbonpanelv1connect.ModpackServiceName,
	carbonpanelv1connect.ModuleServiceName,
	carbonpanelv1connect.NodeServiceName,
	carbonpanelv1connect.ProxyServiceName,
	carbonpanelv1connect.RoleServiceName,
	carbonpanelv1connect.ServerServiceName,
	carbonpanelv1connect.SupportServiceName,
	carbonpanelv1connect.TaskServiceName,
	carbonpanelv1connect.UploadServiceName,
	carbonpanelv1connect.UserServiceName,
}

// TestEveryProcedureAppearsInExactlyOneMap is the CI gate that keeps the
// fail-closed authorization interceptor honest. The interceptor denies any
// procedure that is not present in one of PublicProcedures,
// AuthenticatedOnlyProcedures, or ProcedurePermissions, so a forgotten (or
// duplicated) mapping entry silently degrades security. This test enumerates
// every procedure declared on the registered carbonpanel.v1 services and
// asserts each appears in exactly one of the three tables.
func TestEveryProcedureAppearsInExactlyOneMap(t *testing.T) {
	services := 0
	procedures := 0

	for _, serviceName := range procedureServiceNames {
		desc, err := protoregistry.GlobalFiles.FindDescriptorByName(protoreflect.FullName(serviceName))
		if err != nil {
			t.Fatalf("service %q is not registered in the global proto registry: %v (is the carbonpanelv1 package linked into the test binary?)", serviceName, err)
		}
		service, ok := desc.(protoreflect.ServiceDescriptor)
		if !ok {
			t.Fatalf("descriptor %q is a %T, expected a service descriptor", serviceName, desc)
		}

		services++
		methods := service.Methods()
		for i := 0; i < methods.Len(); i++ {
			method := methods.Get(i)
			procedure := "/" + serviceName + "/" + string(method.Name())
			procedures++

			inPublic := PublicProcedures[procedure]
			inAuthenticatedOnly := AuthenticatedOnlyProcedures[procedure]
			_, inPermissions := ProcedurePermissions[procedure]

			count := 0
			if inPublic {
				count++
			}
			if inAuthenticatedOnly {
				count++
			}
			if inPermissions {
				count++
			}

			switch count {
			case 0:
				t.Errorf("procedure %s is not present in PublicProcedures, AuthenticatedOnlyProcedures, or ProcedurePermissions; it will be denied by default", procedure)
			case 1:
				// Correct: exactly one classification.
			default:
				t.Errorf("procedure %s appears in %d of the three RBAC maps; it must appear in exactly one", procedure, count)
			}
		}
	}

	if services != len(procedureServiceNames) {
		t.Errorf("expected %d services, walked %d", len(procedureServiceNames), services)
	}
	if procedures == 0 {
		t.Fatal("walked zero procedures; the procedure enumeration is broken")
	}
	t.Logf("verified %d procedures across %d services are each mapped exactly once", procedures, services)
}
