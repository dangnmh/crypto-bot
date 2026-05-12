package app

func (cs *CentralStore) SyncTaskNamesForTest() []string {
	names := make([]string, len(cs.syncTasks))
	for i, st := range cs.syncTasks {
		names[i] = st.name
	}
	return names
}
