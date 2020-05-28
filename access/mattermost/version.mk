GITREF=`git describe --dirty --long --tags --match '*mattermost*'`

# $(VERSION_GO) will be written to version.go
VERSION_GO="/* DO NOT EDIT THIS FILE. IT IS GENERATED BY 'make setver'*/\n\n\
package main\n\
const( Version = \"$(VERSION)\" )\n\
// Gitref variable is automatically set to the output of "git-describe" \n\
// during the build process\n\
var Gitref string\n"

# $(GIT_GO) will be written to gitref.go
GITREF_GO="/* DO NOT EDIT THIS FILE. IT IS GENERATED BY make */ \n\n\
package main\n\
func init() { Gitref = \"$(GITREF)\"}  "

#
# setver updates version.go and gitref.go with VERSION and GITREF vars
#
.PHONY:setver
setver:
	@printf $(VERSION_GO) | gofmt > version.go
	@printf $(GITREF_GO) | gofmt > gitref.go
