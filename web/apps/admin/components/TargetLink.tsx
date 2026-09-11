import Link from "next/link";

export function targetHref(type: string, id: string): string | null {
  if (!id) {
    return null;
  }
  if (type === "listing") {
    return `/listings/${encodeURIComponent(id)}`;
  }
  if (type === "public_profile") {
    return `/users/${encodeURIComponent(id)}`;
  }
  return null;
}

export function TargetLink({ type, id }: { type: string; id: string }) {
  const href = targetHref(type, id);
  if (!href) {
    return <code>{id || "—"}</code>;
  }
  return (
    <Link href={href}>
      <code>{id}</code>
    </Link>
  );
}
