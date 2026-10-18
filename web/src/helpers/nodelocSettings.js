// Semi Form treats dots as nested paths unless the whole option key is quoted.
export const nodeLocFormFields = {
  enabled: "['nodeloc.enabled']",
  clientId: "['nodeloc.client_id']",
  clientSecret: "['nodeloc.client_secret']",
};

export function getNodeLocSettingsUpdates(values, savedValues) {
  const options = [];
  const clientId = values['nodeloc.client_id'];
  const clientSecret = values['nodeloc.client_secret'];
  if (
    typeof clientId === 'string' &&
    clientId.trim() !== savedValues['nodeloc.client_id']
  ) {
    options.push({ key: 'nodeloc.client_id', value: clientId.trim() });
  }
  // The server does not return secrets; leaving this field blank preserves it.
  if (typeof clientSecret === 'string' && clientSecret.trim()) {
    options.push({ key: 'nodeloc.client_secret', value: clientSecret.trim() });
  }
  return options;
}
