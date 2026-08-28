-- The ID token, kept for one purpose: RP-initiated logout.
--
-- authentik 2026.8 refuses a logout that carries a post_logout_redirect_uri
-- without an id_token_hint ("OIDC Certification: id_token_hint required with
-- post_logout_redirect_uri", providers/oauth2/views/end_session.py). client_id
-- is not accepted as a substitute, so there is no version of RP-initiated
-- logout with a working redirect that does not store this.
--
-- It is NOT a credential in the sense design D10 refuses. An ID token is an
-- audience-restricted assertion about who authenticated; it cannot be exchanged
-- at the provider for access to anything, unlike the access and refresh tokens
-- D10 was written about. It is cleared the moment it is used.
ALTER TABLE sessions ADD COLUMN logout_hint text;

COMMENT ON COLUMN sessions.logout_hint IS
  'ID token, held solely to satisfy id_token_hint on RP-initiated logout; cleared on use.';
