import { Listing360 } from "./Listing360";

type PageProps = {
  params: Promise<{ listingId: string }>;
};

export default async function Listing360Page({ params }: PageProps) {
  const { listingId } = await params;
  return <Listing360 listingId={listingId} />;
}
