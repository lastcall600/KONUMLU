import { CaseDetail } from "./CaseDetail";

type PageProps = {
  params: Promise<{ caseId: string }>;
};

export default async function CaseDetailPage({ params }: PageProps) {
  const { caseId } = await params;
  return <CaseDetail caseId={caseId} />;
}
