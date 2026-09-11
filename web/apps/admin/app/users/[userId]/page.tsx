import { User360 } from "./User360";

type PageProps = {
  params: Promise<{ userId: string }>;
};

export default async function User360Page({ params }: PageProps) {
  const { userId } = await params;
  return <User360 userId={userId} />;
}
