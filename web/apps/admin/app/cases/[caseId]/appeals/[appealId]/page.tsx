import { AppealDetail } from "./AppealDetail";

type PageProps = {
  params: Promise<{ caseId: string; appealId: string }>;
};

export default async function AppealDetailPage({ params }: PageProps) {
  const { caseId, appealId } = await params;
  return <AppealDetail caseId={caseId} appealId={appealId} />;
}
