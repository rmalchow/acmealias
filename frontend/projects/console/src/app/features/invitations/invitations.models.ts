/** JSON shapes for invitations (`backend/internal/httpapi/admin_invitations.go`). */

/**
 * An outstanding offer of membership, as the ACCOUNT sees it.
 *
 * It names no role, and that absence is the design. An invitation offers membership; what the
 * new member may then do is decided separately, afterwards, by whoever is in a position to decide
 * it — rather than by the person doing the inviting, fourteen days early, against a list of roles
 * they might edit in the meantime.
 *
 * Nothing here can be used to accept: the token exists only in the message that was sent, and only
 * a hash of it is stored.
 */
export interface Invitation {
  id: string;
  email: string;
  /** Display name of whoever issued it, so a member can ask the right person about a name. */
  invitedBy: string;
  createdAt: string;
  /** When a message was last put on the wire. Equal to `createdAt` until somebody re-sends. */
  lastSentAt: string;
  expiresAt: string;
}

/**
 * The same invitation as the INVITED PERSON sees it: which account, and which address it was sent
 * to. Nothing else about the account is disclosed.
 *
 * Both the token route and the signed-in listing answer with this shape, because they are the same
 * offer reached two ways.
 */
export interface InvitationOffer {
  /**
   * Present only on the listing of your OWN invitations, absent from a token resolution — which is
   * the difference between the two ways in.
   *
   * It is not a credential and cannot be used as one. Answering by identifier finds the row only
   * when the invited address is the caller's own verified one, so somebody else's identifier is
   * indistinguishable from one belonging to no invitation at all.
   */
  id?: string;
  account: string;
  accountId: string;
  email: string;
  expiresAt: string;
}
