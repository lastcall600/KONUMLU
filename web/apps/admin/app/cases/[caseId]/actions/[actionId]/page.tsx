import { ActionDetail } from "./ActionDetail";

type PageProps = {
  params: Promise<{ caseId: string; actionId: string }>;
};

export default async function ActionDetailPage({ params }: PageProps) {
  const { caseId, actionId } = await params;
  return <ActionDetail caseId={caseId} actionId={actionId} />;
}
