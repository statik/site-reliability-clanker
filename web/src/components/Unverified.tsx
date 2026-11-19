import { Tag } from "@carbon/react";
import { useSession } from "@/state/session";

const REASON = "This person gave the name. No identity provider confirmed it.";

/** The mark for a name that the person gave and no identity provider confirmed. */
export function UnverifiedTag() {
  return <Tag type="outline" size="sm" title={REASON}>Unverified</Tag>;
}

/** The short name of a user, with the mark when the person gave the name. */
export function UserName({ name }: { name: string }) {
  const { users } = useSession();
  const selfDeclared = users.some((user) => user.name === name && user.self_declared);
  return <>{name}{selfDeclared && <> <UnverifiedTag /></>}</>;
}
