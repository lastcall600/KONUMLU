"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";

type NavItem = {
  href: string;
  label: string;
  exact?: boolean;
};

function itemActive(pathname: string, href: string, exact?: boolean): boolean {
  if (exact) {
    return pathname === href;
  }
  return pathname === href || pathname.startsWith(`${href}/`);
}

export function CaseOpsNav({ caseId }: { caseId: string }) {
  const pathname = usePathname();
  const base = `/cases/${encodeURIComponent(caseId)}`;
  const items: readonly NavItem[] = [
    { href: base, label: "Özet", exact: true },
    { href: `${base}/evidence`, label: "Kanıt" },
    { href: `${base}/actions`, label: "Aksiyonlar" },
    { href: `${base}/appeals`, label: "İtirazlar" },
  ];

  return (
    <nav className="ops-subnav" aria-label="Vaka işlemleri">
      {items.map((item) => (
        <Link
          key={item.href}
          href={item.href}
          aria-current={itemActive(pathname, item.href, item.exact) ? "page" : undefined}
        >
          {item.label}
        </Link>
      ))}
    </nav>
  );
}

export function formatOpsTimestamp(value: string): string {
  if (!value) {
    return "—";
  }
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return value;
  }
  return date.toISOString().replace("T", " ").replace(/\.\d+Z$/, " UTC");
}
