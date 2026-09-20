#!/bin/sh
set -eu
umask 077
/thinkpixelag-migrate
if [ ! -f /state/identity.json ]; then
    /bootstrap prepare /state
fi
if [ ! -f /state/provisioned ]; then
    /bootstrap seed /state
    cp /policies/authorization.rego /trust/authorization.rego
    touch /state/provisioned
fi
cp /state/ca.crt /trust/ca.crt
chmod 644 /trust/ca.crt /trust/authorization.rego
printf 'Sample tenant, approved agent and policy are ready. Existing state retained.\n'
