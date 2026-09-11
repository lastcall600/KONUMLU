"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";

type NavItem = {
  label: string;
  href?: string;
};

const NAV_ITEMS: readonly NavItem[] = [
  { label: "Overview", href: "/" },
  { label: "Users", href: "/users" },
  { label: "Listings", href: "/listings" },
  { label: "Companies" },
  { label: "Trust" },
  { label: "Moderation", href: "/moderation" },
  { label: "Cases", href: "/cases" },
  { label: "Appeals", href: "/appeals" },
  { label: "Finance" },
  { label: "Compliance" },
  { label: "Platform" },
];

function isActive(pathname: string, href: string): boolean {
  if (href === "/") {
    return pathname === "/";
  }
  return pathname === href || pathname.startsWith(`${href}/`);
}

export function ShellNav() {
  const pathname = usePathname();

  return (
    <nav className="shell-nav" aria-label="Management Center">
      <h1>Management Center</h1>
      <ul>
        {NAV_ITEMS.map((item) => (
          <li key={item.label}>
            {item.href ? (
              <Link href={item.href} aria-current={isActive(pathname, item.href) ? "page" : undefined}>
                {item.label}
              </Link>
            ) : (
              item.label
            )}
          </li>
        ))}
      </ul>
    </nav>
  );
}
