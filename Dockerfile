FROM gcr.io/distroless/static-debian11:nonroot
ENTRYPOINT ["/baton-azure-infrastructure"]
COPY baton-azure-infrastructure /